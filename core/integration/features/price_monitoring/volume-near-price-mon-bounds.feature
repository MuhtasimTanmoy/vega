# Test volume and margin when LP volume is pushed inside price monitoring bounds
# and the price monitoring bounds happen to be best bid/ask
Feature: Test margin for lp near price monitoring boundaries

  Background:
    Given the following network parameters are set:
      | name                                                | value |
      | market.value.windowLength                           | 1h    |
      | network.markPriceUpdateMaximumFrequency             | 0s    |
      | limits.markets.maxPeggedOrders                      | 2     |
    And the liquidity monitoring parameters:
            | name               | triggering ratio | time window | scaling factor |
            | lqm-params         | 0.00             | 24h         | 1              |
    And the average block duration is "1"
    And the liquidity sla params named "SLA":
      | price range | commitment min time fraction | performance hysteresis epochs | sla competition factor |
      | 1.0         | 0.5                          | 1                             | 1.0                    |


  Scenario: first scenario for volume at near price monitoring bounds and simple-risk-model

    Given the simple risk model named "simple-risk-model-1":
      | long | short | max move up | min move down | probability of trading |
      | 0.1  | 0.1   | 100         | -100          | 0.2                    |
    And the fees configuration named "fees-config-1":
      | maker fee | infrastructure fee |
      | 0.004     | 0.001              |
    And the price monitoring named "price-monitoring-1":
      | horizon | probability | auction extension |
      | 1       | 0.99        | 300               |
    And the markets:
      | id        | quote name | asset | liquidity monitoring | risk model          | margin calculator         | auction duration | fees          | price monitoring   | data source config     | linear slippage factor | quadratic slippage factor | sla params |
      | ETH/DEC21 | ETH        | ETH   | lqm-params           | simple-risk-model-1 | default-margin-calculator | 1                | fees-config-1 | price-monitoring-1 | default-eth-for-future | 1e6                    | 1e6                       | SLA        |
    And the following network parameters are set:
      | name                                               | value |
      | market.liquidity.providersFeeCalculationTimeStep | 600s  |
    And the parties deposit on asset's general account the following amount:
      | party  | asset | amount    |
      | lp1    | ETH   | 100000000 |
      | party1 | ETH   | 10000000  |
      | party2 | ETH   | 10000000  |

    Given the parties submit the following liquidity provision:
      | id          | party | market id | commitment amount | fee   | lp type    |
      | commitment1 | lp1   | ETH/DEC21 | 78000000          | 0.001 | submission |
      | commitment1 | lp1   | ETH/DEC21 | 78000000          | 0.001 | amendment  |
    And the parties place the following pegged iceberg orders:
      | party  | market id | peak size | minimum visible size | side | pegged reference | volume | offset |
      | lp1    | ETH/DEC21 | 2         | 1                    | buy  | BID              | 97500  | 100    |
      | lp1    | ETH/DEC21 | 2         | 1                    | sell | ASK              | 97500  | 100    |
    And the parties place the following orders:
      | party  | market id | side | volume | price | resulting trades | type       | tif     |
      | party1 | ETH/DEC21 | buy  | 1      | 900   | 0                | TYPE_LIMIT | TIF_GTC |
      | party1 | ETH/DEC21 | buy  | 10     | 1000  | 0                | TYPE_LIMIT | TIF_GTC |
      | party2 | ETH/DEC21 | sell | 1      | 1100  | 0                | TYPE_LIMIT | TIF_GTC |
      | party2 | ETH/DEC21 | sell | 10     | 1000  | 0                | TYPE_LIMIT | TIF_GTC |

    When the opening auction period ends for market "ETH/DEC21"
    Then the auction ends with a traded volume of "10" at a price of "1000"

    And the parties should have the following profit and loss:
      | party  | volume | unrealised pnl | realised pnl |
      | party1 | 10     | 0              | 0            |
      | party2 | -10    | 0              | 0            |

    And the market data for the market "ETH/DEC21" should be:
      | mark price | trading mode            | horizon | min bound | max bound | target stake | supplied stake | open interest |
      | 1000       | TRADING_MODE_CONTINUOUS | 1       | 900       | 1100      | 1000         | 78000000       | 10            |

    And the parties should have the following margin levels:
      | party | market id | maintenance | search   | initial  | release  |
      | lp1   | ETH/DEC21 | 9750000     | 10725000 | 11700000 | 13650000 |

    And the parties should have the following account balances:
      | party | asset | market id | margin   | general  | bond     |
      | lp1   | ETH   | ETH/DEC21 | 11700000 | 10300000 | 78000000 |


    Then the parties place the following orders:
      | party  | market id | side | volume | price | resulting trades | type       | tif     |
      | party1 | ETH/DEC21 | buy  | 1      | 900   | 0                | TYPE_LIMIT | TIF_GTC |

    And the parties should have the following margin levels:
      | party | market id | maintenance | search   | initial  | release  |
      | lp1   | ETH/DEC21 | 9750000     | 10725000 | 11700000 | 13650000 |

    # now we place an order which makes the best bid 901.
    Then the parties place the following orders:
      | party  | market id | side | volume | price | resulting trades | type       | tif     |
      | party1 | ETH/DEC21 | buy  | 1      | 901   | 0                | TYPE_LIMIT | TIF_GTC |

    # margin unaffected as requirement is driven by sell side anyway
    And the parties should have the following margin levels:
      | party | market id | maintenance | search   | initial  | release  |
      | lp1   | ETH/DEC21 | 9750000     | 10725000 | 11700000 | 13650000 |

  Scenario: second scenario for volume at near price monitoring bounds with log-normal

    Given the log normal risk model named "log-normal-risk-model-1":
      | risk aversion | tau     | mu | r | sigma |
      | 0.000001      | 0.00273 | 0  | 0 | 1.2   |
    #rf_short = 0.3611932
    #rf_long = 0.268130582
    And the fees configuration named "fees-config-1":
      | maker fee | infrastructure fee |
      | 0.004     | 0.001              |
    And the price monitoring named "price-monitoring-2":
      | horizon | probability | auction extension |
      | 43200   | 0.982       | 300               |
    And the markets:
      | id         | quote name | asset | risk model              | margin calculator         | auction duration | fees          | price monitoring   | data source config     | linear slippage factor | quadratic slippage factor | sla params |
      | ETH2/MAR22 | ETH2       | ETH2  | log-normal-risk-model-1 | default-margin-calculator | 1                | fees-config-1 | price-monitoring-2 | default-eth-for-future | 1e6                    | 1e6                       | SLA        |
    And the following network parameters are set:
      | name                                               | value |
      | market.liquidity.providersFeeCalculationTimeStep | 600s  |
    And the parties deposit on asset's general account the following amount:
      | party  | asset | amount    |
      | lp1    | ETH2  | 100000000 |
      | party1 | ETH2  | 10000000  |
      | party2 | ETH2  | 10000000  |

    And the parties submit the following liquidity provision:
      | id          | party | market id  | commitment amount | fee   | lp type    |
      | commitment1 | lp1   | ETH2/MAR22 | 50000000          | 0.001 | submission |
      | commitment1 | lp1   | ETH2/MAR22 | 50000000          | 0.001 | amendment  |
    And the parties place the following pegged iceberg orders:
      | party  | market id  | peak size | minimum visible size | side | pegged reference | volume | offset | reference |
      | lp1    | ETH2/MAR22 | 62500     | 1                    | buy  | BID              | 62500  | 100    | ice-buy   |
      | lp1    | ETH2/MAR22 | 41357     | 1                    | sell | ASK              | 41357  | 100    | ice-sell  |
    And the parties place the following orders:
      | party  | market id  | side | volume | price | resulting trades | type       | tif     |
      | party1 | ETH2/MAR22 | buy  | 1      | 900   | 0                | TYPE_LIMIT | TIF_GTC |
      | party1 | ETH2/MAR22 | buy  | 10     | 1000  | 0                | TYPE_LIMIT | TIF_GTC |
      | party2 | ETH2/MAR22 | sell | 1      | 1109  | 0                | TYPE_LIMIT | TIF_GTC |
      | party2 | ETH2/MAR22 | sell | 10     | 1000  | 0                | TYPE_LIMIT | TIF_GTC |

    When the opening auction period ends for market "ETH2/MAR22"
    Then the auction ends with a traded volume of "10" at a price of "1000"

    And the parties should have the following profit and loss:
      | party  | volume | unrealised pnl | realised pnl |
      | party1 | 10     | 0              | 0            |
      | party2 | -10    | 0              | 0            |

    And the market data for the market "ETH2/MAR22" should be:
      | mark price | trading mode            | horizon | min bound | max bound | target stake | supplied stake | open interest |
      | 1000       | TRADING_MODE_CONTINUOUS | 43200   | 900       | 1109      | 36119        | 50000000       | 10            |

    And the order book should have the following volumes for market "ETH2/MAR22":
      | side | price | volume |
      | sell | 1209  | 41357  |
      | sell | 1109  | 1      |
      | buy  | 901   | 0      |
      | buy  | 900   | 1      |
      | buy  | 800   | 62500  |
    # LP_vol: 50000000/1209=41357
    # LP_vol: 50000000/800=62500

    And the parties should have the following margin levels:
      | party | market id  | maintenance | search   | initial  | release  |
      | lp1   | ETH2/MAR22 | 16758162    | 18433978 | 20109794 | 23461426 |

    # Maitenance_margin: 41457*1000*0.3611932+62500*1000*0.268130582=31732147.87
    And the parties should have the following account balances:
      | party | asset | market id  | margin   | general  | bond     |
      | lp1   | ETH2  | ETH2/MAR22 | 20109794 | 29890206 | 50000000 |

    Then the parties place the following orders:
      | party  | market id  | side | volume | price | resulting trades | type       | tif     | reference |
      | party1 | ETH2/MAR22 | buy  | 1      | 900   | 0                | TYPE_LIMIT | TIF_GTC | buy-ref-3 |

    And the market data for the market "ETH2/MAR22" should be:
      | mark price | trading mode            | horizon | min bound | max bound | target stake | supplied stake | open interest |
      | 1000       | TRADING_MODE_CONTINUOUS | 43200   | 900       | 1109      | 36119        | 50000000       | 10            |
    And the order book should have the following volumes for market "ETH2/MAR22":
      | side | price | volume |
      | sell | 1209  | 41357  |
      | sell | 1109  | 1      |
      | buy  | 901   | 0      |
      | buy  | 900   | 2      |
      | buy  | 800   | 62500  |

    And the parties should have the following margin levels:
      | party | market id  | maintenance | search   | initial  | release  |
      | lp1   | ETH2/MAR22 | 16758162    | 18433978 | 20109794 | 23461426 |

    # now we place an order which makes the best bid 901.
    Then the parties place the following orders:
      | party  | market id  | side | volume | price | resulting trades | type       | tif     | reference |
      | party1 | ETH2/MAR22 | buy  | 1      | 901   | 0                | TYPE_LIMIT | TIF_GTC | buy-ref-4 |

    And the market data for the market "ETH2/MAR22" should be:
      | mark price | trading mode            | horizon | min bound | max bound | target stake | supplied stake | open interest |
      | 1000       | TRADING_MODE_CONTINUOUS | 43200   | 900       | 1109      | 36119        | 50000000       | 10            |

    And the parties amend the following pegged iceberg orders:
      | party | reference | size delta |
      | lp1   | ice-buy   | -78        |

    Then the order book should have the following volumes for market "ETH2/MAR22":
      | side | price | volume |
      | sell | 1209  | 41357  |
      | sell | 1109  | 1      |
      | buy  | 901   | 1      |
      | buy  | 900   | 2      |
      | buy  | 801   | 62422  |

    And the parties should have the following margin levels:
      | party | market id  | maintenance | search   | initial  | release  |
      | lp1   | ETH2/MAR22 | 16737248    | 18410972 | 20084697 | 23432147 |

