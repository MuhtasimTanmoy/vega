Feature: If a market insurance pool does not have enough funds to cover a funding payment,
         loss socialisation occurs and the total balances across the network remains constant (0053-PERP-039).

  Background:

    And the perpetual oracles from "0xCAFECAFE1":
      | name        | asset | settlement property | settlement type | schedule property | schedule type  | margin funding factor | interest rate | clamp lower bound | clamp upper bound | quote name | settlement decimals |
      | perp-oracle | USD   | perp.ETH.value      | TYPE_INTEGER    | perp.funding.cue  | TYPE_TIMESTAMP | 1.0                   | 0.75          | 0.1               | 0.9               | ETH        | 18                  |
    And the liquidity sla params named "SLA":
      | price range | commitment min time fraction | performance hysteresis epochs | sla competition factor |
      | 1.0         | 0.5                          | 1                             | 1.0                    |

    And the markets:
      | id        | quote name | asset | risk model                  | margin calculator         | auction duration | fees         | price monitoring | data source config | linear slippage factor | quadratic slippage factor | position decimal places | market type | sla params      |
      | ETH/DEC19 | ETH        | USD   | default-simple-risk-model-3 | default-margin-calculator | 1                | default-none | default-none     | perp-oracle        | 1e6                    | 1e6                       | -3                      | perp        | default-futures |
    And the initial insurance pool balance is "200" for all the markets
    And the following network parameters are set:
      | name                           | value |
      | market.auction.minimumDuration | 1     |
      | limits.markets.maxPeggedOrders | 2     |

  @Perpetual @Liquidation
  Scenario: 
    Given the following network parameters are set:
      | name                                    | value |
      | network.markPriceUpdateMaximumFrequency | 5s    |
    And the parties deposit on asset's general account the following amount:
      | party  | asset | amount    |
      | party1 | USD   | 10000000  |
      | party2 | USD   | 10000000  |
      | party3 | USD   | 10000000  |
      | aux    | USD   | 100000000 |
      | aux2   | USD   | 1515000   |
      | lpprov | USD   | 100000000 |

    When the parties submit the following liquidity provision:
      | id  | party  | market id | commitment amount | fee   | lp type    |
      | lp1 | lpprov | ETH/DEC19 | 10000000          | 0.001 | submission |
    And the parties place the following pegged iceberg orders:
      | party  | market id | peak size | minimum visible size | side | pegged reference | volume | offset |
      | lpprov | ETH/DEC19 | 2         | 1                    | buy  | BID              | 50     | 1      |
      | lpprov | ETH/DEC19 | 2         | 1                    | sell | ASK              | 50     | 1      |

    # place auxiliary orders so we always have best bid and best offer as to not trigger the liquidity auction
    When the parties place the following orders:
      | party | market id | side | volume | price | resulting trades | type       | tif     |
      | aux   | ETH/DEC19 | buy  | 1      | 49    | 0                | TYPE_LIMIT | TIF_GTC |
      | aux   | ETH/DEC19 | sell | 1      | 5001  | 0                | TYPE_LIMIT | TIF_GTC |
      | aux2  | ETH/DEC19 | buy  | 1      | 1000  | 0                | TYPE_LIMIT | TIF_GTC |
      | aux   | ETH/DEC19 | sell | 1      | 1000  | 0                | TYPE_LIMIT | TIF_GTC |

    And the market data for the market "ETH/DEC19" should be:
      | target stake | supplied stake |
      | 1100000      | 10000000       |
    Then the opening auction period ends for market "ETH/DEC19"
    And the trading mode should be "TRADING_MODE_CONTINUOUS" for the market "ETH/DEC19"
    And the settlement account should have a balance of "0" for the market "ETH/DEC19"
    And the cumulated balance for all accounts should be worth "231515200"
    And the insurance pool balance should be "200" for the market "ETH/DEC19"

    # back sure we end the block so we're in a new one after opening auction
    When the network moves ahead "1" blocks

    When the parties place the following orders:
      | party  | market id | side | volume | price | resulting trades | type       | tif     |
      | party1 | ETH/DEC19 | sell | 1      | 1000  | 0                | TYPE_LIMIT | TIF_GTC |
      | party2 | ETH/DEC19 | buy  | 1      | 1000  | 1                | TYPE_LIMIT | TIF_GTC |

    And the settlement account should have a balance of "0" for the market "ETH/DEC19"
    When the parties place the following orders:
      | party  | market id | side | volume | price | resulting trades | type       | tif     |
      | party1 | ETH/DEC19 | sell | 1      | 1200  | 0                | TYPE_LIMIT | TIF_GTC |
      | party3 | ETH/DEC19 | buy  | 1      | 1200  | 1                | TYPE_LIMIT | TIF_GTC |

    And the market data for the market "ETH/DEC19" should be:
      | mark price | trading mode            | target stake | supplied stake | open interest |
      | 1000       | TRADING_MODE_CONTINUOUS | 3300000      | 10000000       | 3             |

    # send in external data to the perpetual market, it should not change anything and a MTM should not happen
    When the network moves ahead "1" blocks
    And the mark price should be "1000" for the market "ETH/DEC19"

    When the oracles broadcast data with block time signed with "0xCAFECAFE1":
      | name             | value                  | time offset |
      | perp.ETH.value   | 3000000000000000000000 | 0s          |
      | perp.funding.cue | 1612998252             | 0s          |
    When the network moves ahead "4" blocks

    #MTM for mark price 1000 to 1200
    And the following transfers should happen:
      | from   | to     | from account            | to account              | market id | amount | asset |
      | party1 | market | ACCOUNT_TYPE_MARGIN     | ACCOUNT_TYPE_SETTLEMENT | ETH/DEC19 | 200000 | USD   |
      | aux    | market | ACCOUNT_TYPE_MARGIN     | ACCOUNT_TYPE_SETTLEMENT | ETH/DEC19 | 200000 | USD   |
      | market | aux2   | ACCOUNT_TYPE_SETTLEMENT | ACCOUNT_TYPE_MARGIN     | ETH/DEC19 | 200000 | USD   |
      | market | party2 | ACCOUNT_TYPE_SETTLEMENT | ACCOUNT_TYPE_MARGIN     | ETH/DEC19 | 200000 | USD   |

    And the settlement account should have a balance of "0" for the market "ETH/DEC19"
    And the insurance pool balance should be "200" for the market "ETH/DEC19"
    And the cumulated balance for all accounts should be worth "231515200"
    When the network moves ahead "1" blocks

    And the parties should have the following account balances:
      | party   | asset | market id | margin     | general  |
      | party1  | USD   | ETH/DEC19 | 9410400    | 389600   |
      | party2  | USD   | ETH/DEC19 | 1539600    | 8659400  |
      | party3  | USD   | ETH/DEC19 | 1539600    | 8459200  |
      | aux     | USD   | ETH/DEC19 | 4841200    | 94958800 |
      | aux2    | USD   | ETH/DEC19 | 1473200    | 241800   |
      | lpprov  | USD   | ETH/DEC19 | 7920000    | 82080000 |

    When the parties place the following orders:
      | party | market id | side | volume | price | resulting trades | type       | tif     |
      | aux   | ETH/DEC19 | sell | 1      | 1500  | 0                | TYPE_LIMIT | TIF_GTC |
      | aux2  | ETH/DEC19 | buy  | 1      | 1500  | 1                | TYPE_LIMIT | TIF_GTC |
    # Allow network close-outs to kick in
    Then the network moves ahead "1" blocks

    Then the parties should have the following margin levels:
      | party  | market id | maintenance | initial |
      | party1 | ETH/DEC19 | 7842000     | 9410400 |
      | party2 | ETH/DEC19 | 1283000     | 1539600 |
      | aux2   | ETH/DEC19 | 1415000     | 1698000 |
    And the mark price should be "1200" for the market "ETH/DEC19"

    And the parties should have the following account balances:
      | party   | asset | market id | margin     | general  |
      | party1  | USD   | ETH/DEC19 | 9410400    | 389600   |
      | party2  | USD   | ETH/DEC19 | 1539600    | 8659400  |
      | party3  | USD   | ETH/DEC19 | 1539600    | 8459200  |
      | aux     | USD   | ETH/DEC19 | 4993200    | 94806800 |
      | aux2    | USD   | ETH/DEC19 | 1698000    | 15500    |
      | lpprov  | USD   | ETH/DEC19 | 7920000    | 82082200 |

    #delta_t = 0.5
    When the oracles broadcast data with block time signed with "0xCAFECAFE1":
      | name             | value                  | time offset |
      | perp.ETH.value   | 3000000000000000000000 | 0s          |
      | perp.funding.cue | 1628766252             | 0s          |

    #funding payment = s_twap * delta_t * interest_rate = 3000*1*0.75 = 225,000 (which is more than the aux2 party has)
    And the following transfers should happen:
      | from   | to     | from account            | to account              | market id | amount  | asset |
      | aux2   | market | ACCOUNT_TYPE_MARGIN     | ACCOUNT_TYPE_SETTLEMENT | ETH/DEC19 | 1698000 | USD   |
      | aux2   | market | ACCOUNT_TYPE_GENERAL    | ACCOUNT_TYPE_SETTLEMENT | ETH/DEC19 | 15500   | USD   |
      | party2 | market | ACCOUNT_TYPE_MARGIN     | ACCOUNT_TYPE_SETTLEMENT | ETH/DEC19 | 900000  | USD   |
      | party3 | market | ACCOUNT_TYPE_MARGIN     | ACCOUNT_TYPE_SETTLEMENT | ETH/DEC19 | 900000  | USD   |
      | market | aux    | ACCOUNT_TYPE_SETTLEMENT | ACCOUNT_TYPE_MARGIN     | ETH/DEC19 | 1756850 | USD   |
      | market | party1 | ACCOUNT_TYPE_SETTLEMENT | ACCOUNT_TYPE_MARGIN     | ETH/DEC19 | 1756850 | USD   |

    Then the parties should have the following margin levels:
      | party | market id | maintenance | initial |
      | aux2  | ETH/DEC19 | 0           | 0       |

    # check that loss socialisation has happened and that the insurance pool has been cleared to indicate 
    # that there wasn't enough in there to cover the funding payment.
    And the insurance pool balance should be "0" for the market "ETH/DEC19"
    And the settlement account should have a balance of "0" for the market "ETH/DEC19"

    # Check we still have a constant total amount in all accounts
    And the cumulated balance for all accounts should be worth "231515200"

    And the parties should have the following account balances:
      | party   | asset | market id | margin     | general  |
      | party1  | USD   | ETH/DEC19 | 9410400    | 2146450  |
      | party2  | USD   | ETH/DEC19 | 1539600    | 7759400  |
      | party3  | USD   | ETH/DEC19 | 1539600    | 7559200  |
      | aux     | USD   | ETH/DEC19 | 9554400    | 92002450 |
      | aux2    | USD   | ETH/DEC19 | 0          | 0        |
      | lpprov  | USD   | ETH/DEC19 | 7920000    | 82082200 |
