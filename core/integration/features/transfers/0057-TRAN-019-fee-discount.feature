Feature: Test one off transfers

Background:
    Given time is updated to "2021-08-26T00:00:00Z"

    Given the fees configuration named "fees-config-1":
      | maker fee | infrastructure fee |
      | 0.005     | 0.002              |
    And the price monitoring named "price-monitoring":
      | horizon | probability | auction extension |
      | 1       | 0.99        | 3                 |

    And the simple risk model named "simple-risk-model-1":
      | long | short | max move up | min move down | probability of trading |
      | 0.2  | 0.1   | 100         | -100          | 0.1                    |

    And the markets:
      | id        | quote name | asset | risk model          | margin calculator         | auction duration | fees          | price monitoring | data source config     | position decimal places | linear slippage factor | quadratic slippage factor | sla params      |
      | ETH/DEC21 | ETH        | ETH   | simple-risk-model-1 | default-margin-calculator | 2                | fees-config-1 | price-monitoring | default-eth-for-future | 2                       | 1e6                    | 1e6                       | default-futures |
    And the following network parameters are set:
      | name                                               | value |
      | market.liquidity.providersFeeCalculationTimeStep | 1s    |

    Given the following network parameters are set:
      | name                                    | value |
      | transfer.fee.factor                     |  1    |
      | network.markPriceUpdateMaximumFrequency | 0s    |
      | transfer.fee.maxQuantumAmount           |  1    |
      | transfer.feeDiscountDecayFraction       |  0.9  |
      | limits.markets.maxPeggedOrders          | 4     |
      | validators.epoch.length                 | 10s   |

    # setup accounts
    Given the parties deposit on asset's general account the following amount:
      | party   | asset | amount    |
      | aux1    | ETH   | 100000000 |
      | aux2    | ETH   | 100000000 |
      | trader3 | ETH   | 10000     |
      | trader4 | ETH   | 10000     |
      | lpprov  | ETH   | 10000000  |
      | f0b40ebdc5b92cf2cf82ff5d0c3f94085d23d5ec2d37d0b929e177c6d4d37e4c  | ETH   | 10000000  |

    Then the parties place the following orders:
      | party | market id | side | volume | price | resulting trades | type       | tif     |
      | aux1  | ETH/DEC21 | buy  | 1000   | 1000  | 0                | TYPE_LIMIT | TIF_GTC |
      | aux2  | ETH/DEC21 | sell | 1000   | 1000  | 0                | TYPE_LIMIT | TIF_GTC |
      | aux1  | ETH/DEC21 | buy  | 100    | 900   | 0                | TYPE_LIMIT | TIF_GTC |
      | aux2  | ETH/DEC21 | sell | 100    | 1100  | 0                | TYPE_LIMIT | TIF_GTC |
    And the parties submit the following liquidity provision:
      | id  | party  | market id | commitment amount | fee | lp type    |
      | lp1 | lpprov | ETH/DEC21 | 90000             | 0.1 | submission |
      | lp1 | lpprov | ETH/DEC21 | 90000             | 0.1 | submission |
    And the parties place the following pegged iceberg orders:
      | party  | market id | peak size | minimum visible size | side | pegged reference | volume | offset |
      | lpprov | ETH/DEC21 | 10000     | 1                    | buy  | BID              | 20000  | 100    |
      | lpprov | ETH/DEC21 | 10000     | 1                    | sell | ASK              | 20000  | 100    |

    Then the opening auction period ends for market "ETH/DEC21"
    And the market data for the market "ETH/DEC21" should be:
      | mark price | trading mode            |
      | 1000       | TRADING_MODE_CONTINUOUS |
    When the parties place the following orders "1" blocks apart:
      | party   | market id | side | volume | price | resulting trades | type       | tif     |
      | f0b40ebdc5b92cf2cf82ff5d0c3f94085d23d5ec2d37d0b929e177c6d4d37e4c | ETH/DEC21 | buy  | 300    | 1002  | 0                | TYPE_LIMIT | TIF_GTC |
      | trader4 | ETH/DEC21 | sell | 400    | 1002  | 1                | TYPE_LIMIT | TIF_GTC |

    Then the following trades should be executed:
      | buyer   | price | size | seller  | aggressor side |
      | f0b40ebdc5b92cf2cf82ff5d0c3f94085d23d5ec2d37d0b929e177c6d4d37e4c | 1002  | 300  | trader4 | sell           |

    # trade_value_for_fee_purposes = size_of_trade * price_of_trade = 3 *1002 = 3006
    # infrastructure_fee = fee_factor[infrastructure] * trade_value_for_fee_purposes = 0.002 * 3006 = 6.012 = 7 (rounded up to nearest whole value)
    # maker_fee =  fee_factor[maker]  * trade_value_for_fee_purposes = 0.005 * 3006 = 15.030 = 16 (rounded up to nearest whole value)
    # liquidity_fee = fee_factor[liquidity] * trade_value_for_fee_purposes = 0 * 3006 = 0

    And the following transfers should happen:
      | from    | to      | from account            | to account                       | market id | amount | asset |
      | trader4 | market  | ACCOUNT_TYPE_GENERAL    | ACCOUNT_TYPE_FEES_MAKER          | ETH/DEC21 | 16     | ETH   |
      | trader4 |         | ACCOUNT_TYPE_GENERAL    | ACCOUNT_TYPE_FEES_INFRASTRUCTURE |           | 7      | ETH   |
      | trader4 | market  | ACCOUNT_TYPE_GENERAL    | ACCOUNT_TYPE_FEES_LIQUIDITY      | ETH/DEC21 | 301    | ETH   |
      | market  | f0b40ebdc5b92cf2cf82ff5d0c3f94085d23d5ec2d37d0b929e177c6d4d37e4c | ACCOUNT_TYPE_FEES_MAKER | ACCOUNT_TYPE_GENERAL             | ETH/DEC21 | 16     | ETH   |


    # make the epoch end so that the fees are registered
    When the network moves ahead "10" blocks
    And the current epoch is "1"

    # move another epoch so that the decay is applied
    When the network moves ahead "10" blocks
    And the current epoch is "2"

    # ???
    When the network moves ahead "10" blocks
    And the current epoch is "3"

@thingg
Scenario: simple successful transfers when (transfer amount * transfer.fee.factor <= transfer.fee.maxQuantumAmount * quantum) (0057-TRAN-001, 0057-TRAN-007, 0057-TRAN-008)


    Given the following network parameters are set:
      | name                                    | value |
      | transfer.fee.factor                     |  0.5  |

    # This party has a fee discount of 324 and 9998686 in their general account
    Given "f0b40ebdc5b92cf2cf82ff5d0c3f94085d23d5ec2d37d0b929e177c6d4d37e4c" should have general account balance of "9998686" for asset "ETH"

    # They will transfers 10000, fee is 0.5 * 10000 = 5000, discount is 0.9*324 
    # 9998686 - 10000 - 4709 = 9983977

    Given the parties submit the following one off transfers:
    | id | from   |  from_account_type    |   to   |   to_account_type    | asset | amount | delivery_time         |
    | 1  | f0b40ebdc5b92cf2cf82ff5d0c3f94085d23d5ec2d37d0b929e177c6d4d37e4c |  ACCOUNT_TYPE_GENERAL | a7c4b181ef9bf5e9029a016f854e4ad471208020fd86187d07f0b420004f06a4 | ACCOUNT_TYPE_GENERAL | ETH  |  10000 | 2021-08-26T00:09:01Z  |
  
    Given time is updated to "2021-08-26T00:10:01Z"
    Then "f0b40ebdc5b92cf2cf82ff5d0c3f94085d23d5ec2d37d0b929e177c6d4d37e4c" should have general account balance of "9983977" for asset "ETH"
    Then "a7c4b181ef9bf5e9029a016f854e4ad471208020fd86187d07f0b420004f06a4" should have general account balance of "10000" for asset "ETH"



@thing
Scenario: simple successful transfers when (transfer amount * transfer.fee.factor <= transfer.fee.maxQuantumAmount * quantum) (0057-TRAN-001, 0057-TRAN-007, 0057-TRAN-008)


    Given the following network parameters are set:
      | name                                    | value |
      | transfer.fee.factor                     |  0.001|

    # This party has a fee discount of 324 and 9998686 in their general account
    Given "f0b40ebdc5b92cf2cf82ff5d0c3f94085d23d5ec2d37d0b929e177c6d4d37e4c" should have general account balance of "9998686" for asset "ETH"

    # They will transfers 10000, fee is 10000 * 0.001 = 10, discount is 0.9*324 > 10 so no fees are paid
    # 9998686 - 10000 - 0 = 9983977

    Given the parties submit the following one off transfers:
    | id | from   |  from_account_type    |   to   |   to_account_type    | asset | amount | delivery_time         |
    | 1  | f0b40ebdc5b92cf2cf82ff5d0c3f94085d23d5ec2d37d0b929e177c6d4d37e4c |  ACCOUNT_TYPE_GENERAL | a7c4b181ef9bf5e9029a016f854e4ad471208020fd86187d07f0b420004f06a4 | ACCOUNT_TYPE_GENERAL | ETH  |  10000 | 2021-08-26T00:09:01Z  |
  
    Given time is updated to "2021-08-26T00:10:01Z"
    Then "f0b40ebdc5b92cf2cf82ff5d0c3f94085d23d5ec2d37d0b929e177c6d4d37e4c" should have general account balance of "9988686" for asset "ETH"
    Then "a7c4b181ef9bf5e9029a016f854e4ad471208020fd86187d07f0b420004f06a4" should have general account balance of "10000" for asset "ETH"