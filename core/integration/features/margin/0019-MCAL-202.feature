Feature: Amending order partially trades, plenty of balance to cover the order amendment + trades + fees
  Background:
    Given the average block duration is "1"
    And the following network parameters are set:
      | name                                    | value |
      | network.markPriceUpdateMaximumFrequency | 0s    |
      | limits.markets.maxPeggedOrders          | 6     |
      | market.auction.minimumDuration          | 1     |
    And the price monitoring named "my-price-monitoring-1":
      | horizon | probability | auction extension |
      | 5       | 0.99        | 6                 |

    And the liquidity monitoring parameters:
      | name       | triggering ratio | time window | scaling factor |
      | lqm-params | 0.00             | 24h         | 1e-9           |
    And the simple risk model named "simple-risk-model":
      | long | short | max move up | min move down | probability of trading |
      | 0.1  | 0.2   | 100         | -100          | 0.2                    |
    And the markets:
      | id        | quote name | asset | liquidity monitoring | risk model        | margin calculator         | auction duration | fees         | price monitoring      | data source config     | linear slippage factor | quadratic slippage factor | position decimal places | sla params      |
      | ETH/FEB23 | ETH        | USD   | lqm-params           | simple-risk-model | default-margin-calculator | 1                | default-none | my-price-monitoring-1 | default-eth-for-future | 0.25                   | 0                         | 2                       | default-futures |

  @MCAL202
  Scenario:
    Given the parties deposit on asset's general account the following amount:
      | party   | asset | amount       |
      | trader1 | USD   | 100000000000 |
      | trader2 | USD   | 100000000000 |
      | trader3 | USD   | 100000000000 |
      | trader4 | USD   | 100000000000 |
      | lprov1  | USD   | 100000000000 |

    And the parties submit the following liquidity provision:
      | id  | party  | market id | commitment amount | fee | lp type    |
      | lp1 | lprov1 | ETH/FEB23 | 1000              | 0.1 | submission |

    And the parties place the following orders with ticks:
      | party   | market id | side | volume | price  | resulting trades | type       | tif     | reference |
      | trader1 | ETH/FEB23 | buy  | 1000   | 14900  | 0                | TYPE_LIMIT | TIF_GTC |           |
      | trader1 | ETH/FEB23 | buy  | 300    | 15600  | 0                | TYPE_LIMIT | TIF_GTC |           |
      | lprov1  | ETH/FEB23 | buy  | 100    | 15700  | 0                | TYPE_LIMIT | TIF_GTC | lp-buy-1  |
      | trader3 | ETH/FEB23 | buy  | 300    | 15800  | 0                | TYPE_LIMIT | TIF_GTC |           |
      | lprov1  | ETH/FEB23 | sell | 300    | 15800  | 0                | TYPE_LIMIT | TIF_GTC | lp-sell-1 |
      | trader2 | ETH/FEB23 | sell | 600    | 15802  | 0                | TYPE_LIMIT | TIF_GTC | t2-sell-1 |
      | trader2 | ETH/FEB23 | sell | 300    | 200000 | 0                | TYPE_LIMIT | TIF_GTC |           |
      | trader2 | ETH/FEB23 | sell | 1000   | 200100 | 0                | TYPE_LIMIT | TIF_GTC |           |

    When the network moves ahead "2" blocks
    Then the market data for the market "ETH/FEB23" should be:
      | mark price | trading mode            | horizon | min bound | max bound | target stake | supplied stake | open interest |
      | 15800      | TRADING_MODE_CONTINUOUS | 5       | 15701     | 15899     | 0            | 1000           | 300           |

    When the parties place the following pegged orders:
      | party  | market id | side | volume | pegged reference | offset | reference |
      | lprov1 | ETH/FEB23 | buy  | 100    | BID              | 10     | buy_peg_1 |
      | lprov1 | ETH/FEB23 | buy  | 200    | BID              | 20     | buy_peg_2 |

    Then the parties should have the following margin levels:
      | party   | market id | maintenance | search | initial | release | margin mode  | margin factor | order |
      | lprov1  | ETH/FEB23 | 9486        | 10434  | 11383   | 13280   | cross margin | 0             | 0     |
      | trader1 | ETH/FEB23 | 20540       | 22594  | 24648   | 28756   | cross margin | 0             | 0     |
      | trader3 | ETH/FEB23 | 5241        | 5765   | 6289    | 7337    | cross margin | 0             | 0     |
    And the parties should have the following account balances:
      | party   | asset | market id | margin | general     | bond |
      | lprov1  | USD   | ETH/FEB23 | 11383  | 99999987617 | 1000 |
      | trader1 | USD   | ETH/FEB23 | 23496  | 99999976504 |      |
      | trader3 | USD   | ETH/FEB23 | 6289   | 99999993711 |      |

    When the parties place the following orders with ticks:
      | party   | market id | side | volume | price | resulting trades | type       | tif     | reference    |
      | trader3 | ETH/FEB23 | buy  | 100    | 15500 | 0                | TYPE_LIMIT | TIF_GTC | t3-to-amend  |
      | trader4 | ETH/FEB23 | sell | 5      | 15801 | 0                | TYPE_LIMIT | TIF_GTC | t4-for-amend |
    Then the parties should have the following margin levels:
      | party   | market id | maintenance | search | initial | release | margin mode  | margin factor | order |
      | trader3 | ETH/FEB23 | 6650        | 7315   | 7980    | 9310    | cross margin | 0             | 0     |
    And the parties should have the following account balances:
      | party   | asset | market id | margin | general     |
      | trader3 | USD   | ETH/FEB23 | 7980   | 99999992020 |

    When the parties submit update margin mode:
      | party   | market    | margin_mode     | margin_factor | error |
      | trader3 | ETH/FEB23 | isolated margin | 0.3           |       |
    Then the parties should have the following margin levels:
      | party   | market id | maintenance | search | initial | release | margin mode     | margin factor | order |
      | trader3 | ETH/FEB23 | 5070        | 0      | 6084    | 0       | isolated margin | 0.3           | 4650  |
    And the parties should have the following account balances:
      | party   | asset | market id | margin | general     | order margin |
      | trader3 | USD   | ETH/FEB23 | 14220  | 99999981130 | 4650         |

    # amend the order to increase the required margin, and the order partially trades (trader4 has a buy position for 5 @ 15801)
    When the parties amend the following orders:
      | party   | reference   | price | size delta | tif     | error |
      | trader3 | t3-to-amend | 15801 | -90        | TIF_GTC |       |
    Then the orders should have the following status:
      | party   | reference   | status        |
      | trader3 | t3-to-amend | STATUS_ACTIVE |
    And the parties should have the following margin levels:
      | party   | market id | maintenance | search | initial | release | margin mode     | margin factor | order |
      | trader3 | ETH/FEB23 | 5152        | 0      | 6182    | 0       | isolated margin | 0.3           | 237   |
    And the parties should have the following account balances:
      | party   | asset | market id | margin | general     | order margin |
      | trader3 | USD   | ETH/FEB23 | 14457  | 99999985226 | 237          |

