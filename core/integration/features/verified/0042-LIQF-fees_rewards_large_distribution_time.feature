
Feature: Test liquidity provider reward distribution; Check what happens when distribution period is large (both in genesis)

  Background:

    Given the simple risk model named "simple-risk-model-1":
      | long | short | max move up | min move down | probability of trading |
      | 0.1  | 0.1   | 500         | 500           | 0.1                    |
    And the fees configuration named "fees-config-1":
      | maker fee | infrastructure fee |
      | 0.0004    | 0.001              |
    And the price monitoring named "price-monitoring":
      | horizon | probability | auction extension |
      | 1       | 0.99        | 3                 |
    And the following network parameters are set:
      | name                                                | value |
      | market.value.windowLength                           | 1h    |
      | network.markPriceUpdateMaximumFrequency             | 0s    |
      | limits.markets.maxPeggedOrders                      | 4     |
    Given the liquidity monitoring parameters:
      | name               | triggering ratio | time window | scaling factor |
      | lqm-params         | 0.0              | 24h         | 1.0            |
    And the liquidity sla params named "SLA":
      | price range | commitment min time fraction | performance hysteresis epochs | sla competition factor |
      | 1.0         | 0.5                          | 1                             | 1.0                    |
    And the markets:
      | id        | quote name | asset | liquidity monitoring | risk model          | margin calculator         | auction duration | fees          | price monitoring | data source config     | linear slippage factor | quadratic slippage factor | sla params |
      | ETH/MAR22 | USD        | USD   | lqm-params           | simple-risk-model-1 | default-margin-calculator | 2                | fees-config-1 | price-monitoring | default-eth-for-future | 1e6                    | 1e6                       | SLA        |
    And the following network parameters are set:
      | name                                               | value    |
      | market.liquidity.providersFeeCalculationTimeStep | 24h0m0s  |

    Given the average block duration is "2"

  Scenario: 001: 1 LP joining at start, checking liquidity rewards over 3 periods, 1 period with no trades (0042-LIQF-006)
    # setup accounts
    Given the parties deposit on asset's general account the following amount:
      | party  | asset | amount     |
      | lp1 | USD | 2000000000 |
      | party1 | USD   | 100000000  |
      | party2 | USD   | 100000000  |
      | party3 | USD | 100000000 |

    And the parties submit the following liquidity provision:
      | id  | party | market id | commitment amount | fee   | lp type    |
      | lp1 | lp1   | ETH/MAR22 | 10000             | 0.001 | submission |

    And the parties place the following pegged iceberg orders:
      | party | market id | peak size | minimum visible size | side | pegged reference | volume     | offset |
      | lp1 | ETH/MAR22 | 4 | 1 | buy  | BID | 4 | 2 |
      | lp1 | ETH/MAR22 | 7 | 1 | buy  | MID | 7 | 1 |
      | lp1 | ETH/MAR22 | 4 | 1 | sell | ASK | 4 | 2 |
      | lp1 | ETH/MAR22 | 7 | 1 | sell | MID | 7 | 1 |
 
    Then the parties place the following orders:
      | party  | market id | side | volume | price | resulting trades | type       | tif     |
      | party1 | ETH/MAR22 | buy  | 1      | 900   | 0                | TYPE_LIMIT | TIF_GTC |
      | party1 | ETH/MAR22 | buy  | 10     | 1000  | 0                | TYPE_LIMIT | TIF_GTC |
      | party2 | ETH/MAR22 | sell | 1      | 1100  | 0                | TYPE_LIMIT | TIF_GTC |
      | party2 | ETH/MAR22 | sell | 10     | 1000  | 0                | TYPE_LIMIT | TIF_GTC |

    Then the opening auction period ends for market "ETH/MAR22"

    And the following trades should be executed:
      | buyer  | price | size | seller |
      | party1 | 1000  | 10   | party2 |

    And the market data for the market "ETH/MAR22" should be:
      | mark price | trading mode            | horizon | min bound | max bound | target stake | supplied stake | open interest |
      | 1000 | TRADING_MODE_CONTINUOUS | 1 | 500 | 1500 | 1000 | 10000 | 10 |

    Then the order book should have the following volumes for market "ETH/MAR22":
      | side | price | volume |
      | buy  | 898   | 4      |
      | buy  | 900   | 1      |
      | buy  | 999   | 7      |
      | sell | 1001  | 7      |
      | sell | 1100  | 1      |
      | sell | 1102  | 4      |

    #volume = ceiling(liquidity_obligation x liquidity-normalised-proportion / probability_of_trading / price)
    #for any price better than the bid price or better than the ask price it returns 0.5
    #for any price in within 500 price ticks from the best bid/ask (i.e. worse than) it returns the probability as returned by the risk model (in this case 0.1 scaled by 0.5.
    #priceLvel at 898:10000*(1/3)/898=4
    #priceLvel at 999:10000*(2/3)/999=7
    #priceLvel at 1001:10000*(2/3)/1001=7
    #priceLvel at 1102:10000*(1/3)/1102=4

    And the liquidity provider fee shares for the market "ETH/MAR22" should be:
      | party | equity like share | average entry valuation |
      | lp1   | 1                 | 10000                   |


    And the parties should have the following account balances:
      | party  | asset | market id | margin | general   | bond  |
      | lp1 | USD | ETH/MAR22 | 1320 | 1999988680 | 10000 |
      | party1 | USD   | ETH/MAR22 | 1704   | 99998296  |       |
      | party2 | USD   | ETH/MAR22 | 1692   | 99998308  |       |

    # party1 margin = 11*1000*0.1 + 10*(1000-968) = 1420
    # party2 margin = 11*1000*0.1 + 10*(1031-1000)= 1410
    Then the parties should have the following margin levels:
      | party  | market id | maintenance | initial |
      | party1 | ETH/MAR22 | 1420        | 1704    |
      | party2 | ETH/MAR22 | 1410        | 1692    |

    Then the network moves ahead "1" blocks

    And the price monitoring bounds for the market "ETH/MAR22" should be:
      | min bound | max bound |
      | 500       | 1500      |

    And the liquidity fee factor should be "0.001" for the market "ETH/MAR22"

    Then the parties place the following orders:
      | party  | market id | side | volume | price | resulting trades | type       | tif     | reference   |
      | party1 | ETH/MAR22 | sell | 20     | 1000  | 0                | TYPE_LIMIT | TIF_GTC | party1-sell |
      | party2 | ETH/MAR22 | buy  | 20     | 1000  | 2                | TYPE_LIMIT | TIF_GTC | party2-buy  |

    And the parties should have the following account balances:
      | party  | asset | market id | margin | general   | bond  |
      | lp1 | USD | ETH/MAR22 | 1320 | 1999988683 | 10000 |
      | party1 | USD | ETH/MAR22 | 2400 | 99997606  |       |
      | party2 | USD | ETH/MAR22 | 2400 | 99997551  |       |

    Then the parties should have the following account balances:
      | party | asset | market id | margin | general   | bond  |
      | lp1 | USD | ETH/MAR22 | 1320 | 1999988683 | 10000 |
    When the network moves ahead "2" blocks

    And the trading mode should be "TRADING_MODE_CONTINUOUS" for the market "ETH/MAR22"
    And the accumulated liquidity fees should be "20" for the market "ETH/MAR22"

    # opening auction + time window
    Then time is updated to "2019-11-30T00:10:05Z"

    # lp fee got cumulated since the distribution period is large
    And the accumulated liquidity fees should be "20" for the market "ETH/MAR22"
    Then time is updated to "2019-11-30T00:20:05Z"

    And the market data for the market "ETH/MAR22" should be:
      | mark price | trading mode            | horizon | min bound | max bound | target stake | supplied stake | open interest |
      | 1000       | TRADING_MODE_CONTINUOUS | 1       | 483       | 1482      | 1000         | 10000          | 10            |

    Then the order book should have the following volumes for market "ETH/MAR22":
      | side | price | volume |
      | buy  | 898   | 4      |
      | buy  | 900   | 1      |
      | buy  | 949   | 7      |
      | sell | 951   | 0      |
      | sell | 1000  | 7      |
      | sell | 1002  | 4      |
      | sell | 1100  | 1      |

    When the parties place the following orders:
      | party  | market id | side | volume | price | resulting trades | type       | tif     | reference  |
      | party2 | ETH/MAR22 | buy  | 7 | 1002 | 1 | TYPE_LIMIT | TIF_GTC | party1-buy  |
      | party2 | ETH/MAR22 | sell | 4 | 1100 | 0 | TYPE_LIMIT | TIF_GTC | party2-sell |

    Then the following transfers should happen:
      | from   | to     | from account         | to account                  | market id | amount | asset |
      | party2 | market | ACCOUNT_TYPE_GENERAL | ACCOUNT_TYPE_FEES_LIQUIDITY | ETH/MAR22 | 7      | USD   |

# this transfer goes from party2’s general account to market liquidity fee account
    And the accumulated liquidity fees should be "27" for the market "ETH/MAR22"
    And the party "lp1" lp liquidity fee account balance should be "0" for the market "ETH/MAR22"

#lp fee got paid from market liquidity fee account to lp1 liquidity fee account when time is over the "market.liquidity.providers.fee.calculationTimeStep"
    Then time is updated to "2024-12-30T00:30:05Z"

    And the accumulated liquidity fees should be "0" for the market "ETH/MAR22"
    And the party "lp1" lp liquidity fee account balance should be "27" for the market "ETH/MAR22"

    Then the following transfers should happen:
      | from   | to  | from account                | to account                     | market id | amount | asset |
      | market | lp1 | ACCOUNT_TYPE_FEES_LIQUIDITY | ACCOUNT_TYPE_LP_LIQUIDITY_FEES | ETH/MAR22 | 27     | USD   |

    Then the parties should have the following profit and loss:
      | party | volume | unrealised pnl | realised pnl |
      | lp1   | -7     | -343           | 0            |

    Then the parties should have the following account balances:
      | party | asset | market id | margin     | general |
      | lp1   | USD   | ETH/MAR22 | 1999999660 | 0       |
