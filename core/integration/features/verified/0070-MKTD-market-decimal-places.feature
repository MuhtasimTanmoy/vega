Feature: Allow markets to be specified with a smaller number of decimal places than the underlying settlement asset

    Background:
        Given the following network parameters are set:
            | name                                          | value |
            | market.liquidity.bondPenaltyParameter       | 0.2   |
            | limits.markets.maxPeggedOrders                | 1500  |
            | network.markPriceUpdateMaximumFrequency       | 0s    |
            | limits.markets.maxPeggedOrders                | 12    |
        Given the liquidity monitoring parameters:
            | name               | triggering ratio | time window | scaling factor |
            | lqm-params         | 0.1              | 24h         | 1.0            |

        And the following assets are registered:
            | id  | decimal places |
            | ETH | 5              |
            | USD | 2              |
        And the average block duration is "1"
        And the log normal risk model named "log-normal-risk-model-1":
            | risk aversion | tau | mu | r | sigma |
            | 0.000001      | 0.1 | 0  | 0 | 1.0   |
        And the fees configuration named "fees-config-1":
            | maker fee | infrastructure fee |
            | 0.004     | 0.001              |
        And the price monitoring named "price-monitoring-1":
            | horizon | probability | auction extension |
            | 1       | 0.99        | 300               |
        And the markets:
            | id        | quote name | asset | liquidity monitoring | risk model              | margin calculator         | auction duration | fees          | price monitoring   | data source config     | decimal places | position decimal places | linear slippage factor | quadratic slippage factor | sla params      |
            | ETH/MAR22 | ETH        | USD   | lqm-params           | log-normal-risk-model-1 | default-margin-calculator | 1                | fees-config-1 | price-monitoring-1 | default-eth-for-future | 0              | 0                       | 1e6                    | 1e6                       | default-futures |
            | USD/DEC19 | USD        | ETH   | lqm-params           | log-normal-risk-model-1 | default-margin-calculator | 1                | default-none  | price-monitoring-1 | default-usd-for-future | 3              | 3                       | 1e6                    | 1e6                       | default-futures |
            | USD/DEC20 | USD        | ETH   | lqm-params           | log-normal-risk-model-1 | default-margin-calculator | 1                | default-none  | price-monitoring-1 | default-usd-for-future | 5              | 5                       | 1e6                    | 1e6                       | default-futures |
            | USD/DEC21 | USD        | ETH   | lqm-params           | log-normal-risk-model-1 | default-margin-calculator | 1                | default-none  | price-monitoring-1 | default-usd-for-future | 5              | 3                       | 1e6                    | 1e6                       | default-futures |
        And the parties deposit on asset's general account the following amount:
            | party  | asset | amount    |
            | party0 | USD   | 5000000   |
            | party0 | ETH   | 5000000   |
            | party1 | USD   | 100000000 |
            | party1 | ETH   | 100000000 |
            | party2 | USD   | 100000000 |
            | party2 | ETH   | 100000000 |
            | party3 | USD   | 100000000 |
            | lpprov | ETH   | 100000000 |
            | lpprov | USD   | 100000000 |

    @SLABug
    Scenario: 001: Markets with different precisions trade at the same price

        Given  the parties submit the following liquidity provision:
            | id  | party  | market id | commitment amount | fee   | lp type    |
            | lp0 | party0 | USD/DEC20 | 1000              | 0.001 | submission |
            | lp0 | party0 | USD/DEC20 | 1000              | 0.001 | submission |
            | lp1 | party0 | USD/DEC21 | 1000              | 0.001 | submission |
            | lp1 | party0 | USD/DEC21 | 1000              | 0.001 | submission |
            | lp2 | party0 | USD/DEC19 | 1000              | 0.001 | submission |
            | lp2 | party0 | USD/DEC19 | 1000              | 0.001 | submission |
            | lp3 | lpprov | USD/DEC20 | 4000              | 0.001 | submission |
            | lp3 | lpprov | USD/DEC20 | 4000              | 0.001 | submission |
            | lp4 | lpprov | USD/DEC21 | 4000              | 0.001 | submission |
            | lp4 | lpprov | USD/DEC21 | 4000              | 0.001 | submission |
            | lp5 | lpprov | USD/DEC19 | 4000              | 0.001 | submission |
            | lp5 | lpprov | USD/DEC19 | 4000              | 0.001 | submission |
        And the parties place the following pegged iceberg orders:
            | party  | market id | peak size | minimum visible size | side | pegged reference | volume     | offset |
            | party0 | USD/DEC20 | 20 | 1 | sell | ASK | 100 | 20 |
            | party0 | USD/DEC20 | 20 | 1 | buy  | BID | 100 | 20 |
            | party0 | USD/DEC21 | 20 | 1 | sell | ASK | 100 | 20 |
            | party0 | USD/DEC21 | 20 | 1 | buy  | BID | 100 | 20 |
            | party0 | USD/DEC19 | 20 | 1 | sell | ASK | 100 | 20 |
            | party0 | USD/DEC19 | 20 | 1 | buy  | BID | 100 | 20 |
            | lpprov | USD/DEC20 | 20 | 1 | sell | ASK | 100 | 20 |
            | lpprov | USD/DEC20 | 20 | 1 | buy  | BID | 100 | 20 |
            | lpprov | USD/DEC21 | 20 | 1 | sell | ASK | 100 | 20 |
            | lpprov | USD/DEC21 | 20 | 1 | buy  | BID | 100 | 20 |
            | lpprov | USD/DEC19 | 20 | 1 | sell | ASK | 100 | 20 |
            | lpprov | USD/DEC19 | 20 | 1 | buy  | BID | 100 | 20 |

        And the parties place the following orders:
            | party  | market id | side | volume | price  | resulting trades | type       | tif     | reference   |
            | party1 | USD/DEC21 | buy  | 10     | 100000 | 0                | TYPE_LIMIT | TIF_GTC | buy-ref-2   |
            | party2 | USD/DEC21 | sell | 10     | 100000 | 0                | TYPE_LIMIT | TIF_GTC | sell-ref-3  |
            | party1 | USD/DEC20 | buy  | 1000   | 100000 | 0                | TYPE_LIMIT | TIF_GTC | buy-ref-2   |
            | party2 | USD/DEC20 | sell | 1000   | 100000 | 0                | TYPE_LIMIT | TIF_GTC | sell-ref-3  |
            | party1 | USD/DEC19 | buy  | 10     | 1000   | 0                | TYPE_LIMIT | TIF_GTC | buy-ref-2a  |
            | party2 | USD/DEC19 | sell | 10     | 1000   | 0                | TYPE_LIMIT | TIF_GTC | sell-ref-3a |
            | party0 | USD/DEC21 | buy  | 1      | 90000  | 0                | TYPE_LIMIT | TIF_GTC | buy-ref-1   |
            | party0 | USD/DEC21 | sell | 1      | 110000 | 0                | TYPE_LIMIT | TIF_GTC | sell-ref-2  |
            | party0 | USD/DEC20 | buy  | 1      | 90000  | 0                | TYPE_LIMIT | TIF_GTC | buy-ref-1   |
            | party0 | USD/DEC20 | sell | 1      | 110000 | 0                | TYPE_LIMIT | TIF_GTC | sell-ref-2  |
            | party0 | USD/DEC19 | buy  | 1      | 900    | 0                | TYPE_LIMIT | TIF_GTC | buy-ref-1a  |
            | party0 | USD/DEC19 | sell | 1      | 1100   | 0                | TYPE_LIMIT | TIF_GTC | sell-ref-2a |

        Then the market data for the market "USD/DEC19" should be:
            | target stake | supplied stake |
            | 3556         | 5000           |
        Then the market data for the market "USD/DEC20" should be:
            | target stake | supplied stake |
            | 3556         | 5000           |
        Then the market data for the market "USD/DEC21" should be:
            | target stake | supplied stake |
            | 3556         | 5000           |

        When the network moves ahead "2" blocks
        Then the trading mode should be "TRADING_MODE_CONTINUOUS" for the market "USD/DEC21"
        And the trading mode should be "TRADING_MODE_CONTINUOUS" for the market "USD/DEC20"
        And the trading mode should be "TRADING_MODE_CONTINUOUS" for the market "USD/DEC19"

        Then the parties should have the following account balances:
            | party  | asset | market id | margin | general | bond |
            | party0 | ETH   | USD/DEC21 | 43110  | 4910348 | 1000 |
            | party1 | ETH   | USD/DEC21 | 961    | 0       |      |
            | party2 | ETH   | USD/DEC21 | 4268   | 0       |      |
            | party0 | ETH   | USD/DEC20 | 432    | 4910348 | 1000 |
            | party1 | ETH   | USD/DEC20 | 0      | 0       |      |
            | party2 | ETH   | USD/DEC20 | 0      | 0       |      |
            | party0 | ETH   | USD/DEC19 | 43110  | 4910348 | 1000 |
            | party1 | ETH   | USD/DEC19 | 1102   | 0       |      |
            | party2 | ETH   | USD/DEC19 | 4268   | 0       |      |

    @Liquidation
    Scenario: 002: Users engage in a USD market auction, (0070-MKTD-003, 0070-MKTD-008)
        Given the parties submit the following liquidity provision:
            | id  | party  | market id | commitment amount | fee   | lp type    |
            | lp1 | party0 | ETH/MAR22 | 35569             | 0.001 | submission |
            | lp1 | party0 | ETH/MAR22 | 35569             | 0.001 | amendment  |
        And the parties place the following pegged iceberg orders:
            | party  | market id | peak size | minimum visible size | side | pegged reference | volume | offset |
            | party0 | ETH/MAR22 | 2         | 1                    | sell | ASK              | 500    | 20     |
            | party0 | ETH/MAR22 | 2         | 1                    | buy  | BID              | 500    | 20     |
        And the parties place the following orders:
            | party  | market id | side | volume | price | resulting trades | type       | tif     | reference  |
            | party1 | ETH/MAR22 | buy  | 1      | 9     | 0                | TYPE_LIMIT | TIF_GTC | buy-ref-1  |
            | party1 | ETH/MAR22 | buy  | 1      | 9     | 0                | TYPE_LIMIT | TIF_GTC | buy-ref-1  |
            | party1 | ETH/MAR22 | buy  | 10     | 10    | 0                | TYPE_LIMIT | TIF_GTC | buy-ref-2  |
            | party2 | ETH/MAR22 | sell | 10     | 10    | 0                | TYPE_LIMIT | TIF_GTC | sell-ref-3 |
            | party2 | ETH/MAR22 | sell | 1      | 10    | 0                | TYPE_LIMIT | TIF_GTC | sell-ref-1 |
            | party2 | ETH/MAR22 | sell | 1      | 11    | 0                | TYPE_LIMIT | TIF_GTC | sell-ref-2 |

        When the opening auction period ends for market "ETH/MAR22"
        Then the trading mode should be "TRADING_MODE_CONTINUOUS" for the market "ETH/MAR22"

        And the parties should have the following account balances:
            | party  | asset | market id | margin  | general  | bond  |
            | party0 | USD   | ETH/MAR22 | 2134142 | 2830289  | 35569 |
            | party1 | USD   | ETH/MAR22 | 0       | 0        |       |
            | party2 | USD   | ETH/MAR22 | 70539   | 99929461 |       |
        # party1 is closed out
        And the following trades should be executed:
            | buyer   | price | size | seller |
            | party1  | 10    | 10   | party2 |
            | network | 10    | 10   | party1 |

    Scenario: 003: Users engage in an ETH market auction, (0070-MKTD-003, 0070-MKTD-008)
        Given the parties submit the following liquidity provision:
            | id  | party  | market id | commitment amount | fee   | lp type    |
            | lp1 | party0 | USD/DEC19 | 50000             | 0.001 | submission |
            | lp1 | party0 | USD/DEC19 | 50000             | 0.001 | amendment  |
        And the parties place the following pegged iceberg orders:
            | party  | market id | peak size | minimum visible size | side | pegged reference | volume | offset |
            | party0 | USD/DEC19 | 2         | 1                    | sell | ASK              | 500    | 20     |
            | party0 | USD/DEC19 | 2         | 1                    | buy  | BID              | 500    | 20     |
        And the parties place the following orders:
            | party  | market id | side | volume | price | resulting trades | type       | tif     | reference  |
            | party1 | USD/DEC19 | buy  | 1      | 900   | 0                | TYPE_LIMIT | TIF_GTC | buy-ref-1  |
            | party1 | USD/DEC19 | buy  | 1      | 900   | 0                | TYPE_LIMIT | TIF_GTC | buy-ref-1  |
            | party1 | USD/DEC19 | buy  | 10     | 1000  | 0                | TYPE_LIMIT | TIF_GTC | buy-ref-2  |
            | party2 | USD/DEC19 | sell | 10     | 1000  | 0                | TYPE_LIMIT | TIF_GTC | sell-ref-3 |
            | party2 | USD/DEC19 | sell | 1      | 1010  | 0                | TYPE_LIMIT | TIF_GTC | sell-ref-1 |
            | party2 | USD/DEC19 | sell | 1      | 1100  | 0                | TYPE_LIMIT | TIF_GTC | sell-ref-2 |

        When the opening auction period ends for market "USD/DEC19"
        Then the auction ends with a traded volume of "10" at a price of "1000"
        And the parties should have the following account balances:
            | party  | asset | market id | margin | general  | bond  |
            | party0 | ETH | USD/DEC19 | 213415 | 4736585 | 50000 |
            | party1 | ETH   | USD/DEC19 | 1292   | 99998708 |       |
            | party2 | ETH   | USD/DEC19 | 5169   | 99994831 |       |
        And the following trades should be executed:
            | buyer  | price | size | seller |
            | party1 | 1000  | 10   | party2 |

    Scenario: 004: Users engage in an ETH market auction with full decimal places, (0070-MKTD-003, 0070-MKTD-008)

        Given  the parties submit the following liquidity provision:
            | id  | party  | market id | commitment amount | fee   | lp type    |
            | lp1 | party0 | USD/DEC20 | 500               | 0.001 | submission |
            | lp1 | party0 | USD/DEC20 | 500               | 0.001 | amendment  |
        And the parties place the following pegged iceberg orders:
            | party  | market id | peak size | minimum visible size | side | pegged reference | volume | offset |
            | party0 | USD/DEC20 | 2         | 1                    | sell | ASK              | 500    | 20     |
            | party0 | USD/DEC20 | 2         | 1                    | buy  | BID              | 500    | 20     |
        And the parties place the following orders:
            | party  | market id | side | volume | price  | resulting trades | type       | tif     | reference  |
            | party1 | USD/DEC20 | buy  | 1      | 90000  | 0                | TYPE_LIMIT | TIF_GTC | buy-ref-1  |
            | party1 | USD/DEC20 | buy  | 1      | 90000  | 0                | TYPE_LIMIT | TIF_GTC | buy-ref-1  |
            | party1 | USD/DEC20 | buy  | 10     | 100000 | 0                | TYPE_LIMIT | TIF_GTC | buy-ref-2  |
            | party2 | USD/DEC20 | sell | 10     | 100000 | 0                | TYPE_LIMIT | TIF_GTC | sell-ref-3 |
            | party2 | USD/DEC20 | sell | 1      | 101000 | 0                | TYPE_LIMIT | TIF_GTC | sell-ref-1 |
            | party2 | USD/DEC20 | sell | 1      | 110000 | 0                | TYPE_LIMIT | TIF_GTC | sell-ref-2 |

        When the opening auction period ends for market "USD/DEC20"
        Then the auction ends with a traded volume of "10" at a price of "100000"
        And the parties should have the following account balances:
            | party  | asset | market id | margin | general  | bond |
            | party0 | ETH | USD/DEC20 | 2134 | 4997366  | 500 |
            | party1 | ETH | USD/DEC20 | 12   | 99999988 |     |
            | party2 | ETH | USD/DEC20 | 52   | 99999948 |     |
        And the following trades should be executed:
            | buyer  | price  | size | seller |
            | party1 | 100000 | 10   | party2 |

    @SLABug
    Scenario: 005: User tops up markets with differing precisions with the same asset + amount, should result in identical margin changes, (0070-MKTD-004)

        Given  the parties submit the following liquidity provision:
            | id  | party  | market id | commitment amount | fee   | lp type    |
            | lp1 | party0 | USD/DEC20 | 100000            | 0.001 | submission |
            | lp1 | party0 | USD/DEC20 | 100000            | 0.001 | amendment  |
            | lp2 | party0 | USD/DEC19 | 5000              | 0.001 | submission |
            | lp2 | party0 | USD/DEC19 | 5000              | 0.001 | amendment  |
        And the parties place the following pegged iceberg orders:
            | party  | market id | peak size | minimum visible size | side | pegged reference | volume | offset |
            | party0 | USD/DEC20 | 2         | 1                    | sell | ASK              | 100    | 20     |
            | party0 | USD/DEC20 | 2         | 1                    | buy  | BID              | 100    | 20     |
            | party0 | USD/DEC19 | 2         | 1                    | sell | ASK              | 100    | 20     |
            | party0 | USD/DEC19 | 2         | 1                    | buy  | BID              | 100    | 20     |

        And the parties place the following orders:
            | party  | market id | side | volume | price  | resulting trades | type       | tif     | reference   |
            | party1 | USD/DEC20 | buy  | 1      | 90000  | 0                | TYPE_LIMIT | TIF_GTC | buy-ref-1   |
            | party1 | USD/DEC20 | buy  | 1      | 90000  | 0                | TYPE_LIMIT | TIF_GTC | buy-ref-1   |
            | party1 | USD/DEC20 | buy  | 10     | 100000 | 0                | TYPE_LIMIT | TIF_GTC | buy-ref-2   |
            | party2 | USD/DEC20 | sell | 10     | 100000 | 0                | TYPE_LIMIT | TIF_GTC | sell-ref-3  |
            | party2 | USD/DEC20 | sell | 1      | 101000 | 0                | TYPE_LIMIT | TIF_GTC | sell-ref-1  |
            | party2 | USD/DEC20 | sell | 1      | 110000 | 0                | TYPE_LIMIT | TIF_GTC | sell-ref-2  |
            | party1 | USD/DEC19 | buy  | 1      | 900    | 0                | TYPE_LIMIT | TIF_GTC | buy-ref-1a  |
            | party1 | USD/DEC19 | buy  | 1      | 900    | 0                | TYPE_LIMIT | TIF_GTC | buy-ref-1a  |
            | party1 | USD/DEC19 | buy  | 10     | 1000   | 0                | TYPE_LIMIT | TIF_GTC | buy-ref-2a  |
            | party2 | USD/DEC19 | sell | 10     | 1000   | 0                | TYPE_LIMIT | TIF_GTC | sell-ref-3a |
            | party2 | USD/DEC19 | sell | 1      | 1010   | 0                | TYPE_LIMIT | TIF_GTC | sell-ref-1a |
            | party2 | USD/DEC19 | sell | 1      | 1100   | 0                | TYPE_LIMIT | TIF_GTC | sell-ref-2a |

        Then the market data for the market "USD/DEC19" should be:
            | target stake | supplied stake |
            | 3556         | 5000           |

        When the network moves ahead "2" blocks

# party1 has position +10 and mark price 100000

        Then the parties should have the following account balances:
            | party  | asset | market id | margin | general  | bond   |
            | party0 | ETH   | USD/DEC20 | 427    | 4851889  | 100000 |
            | party1 | ETH   | USD/DEC20 | 12     | 99998696 |        |
            | party2 | ETH   | USD/DEC20 | 52     | 99994779 |        |
            | party0 | ETH   | USD/DEC19 | 42684  | 4851889  | 5000   |
            | party1 | ETH   | USD/DEC19 | 1292   | 99998696 |        |
            | party2 | ETH   | USD/DEC19 | 5169   | 99994779 |        |

        When the parties deposit on asset's general account the following amount:
            | party  | asset | amount |
            | party0 | ETH   | 1000   |
            | party1 | ETH   | 1000   |
            | party2 | ETH   | 1000   |
        Then the parties should have the following account balances:
            | party  | asset | market id | margin | general  | bond   |
            | party0 | ETH   | USD/DEC20 | 427    | 4852889  | 100000 |
            | party1 | ETH   | USD/DEC20 | 12     | 99999696 |        |
            | party2 | ETH   | USD/DEC20 | 52     | 99995779 |        |
            | party0 | ETH   | USD/DEC19 | 42684  | 4852889  | 5000   |
            | party1 | ETH   | USD/DEC19 | 1292   | 99999696 |        |
            | party2 | ETH   | USD/DEC19 | 5169   | 99995779 |        |

    @SLABug
    Scenario: 006: User checks prices after opening auction, (0070-MKTD-005)

        Given  the parties submit the following liquidity provision:
            | id  | party  | market id | commitment amount | fee   | lp type    |
            | lp1 | party0 | USD/DEC20 | 100000            | 0.001 | submission |
            | lp1 | party0 | USD/DEC20 | 100000            | 0.001 | amendment  |
            | lp2 | party0 | USD/DEC19 | 5000              | 0.001 | submission |
            | lp2 | party0 | USD/DEC19 | 5000              | 0.001 | amendment  |
        And the parties place the following pegged iceberg orders:
            | party  | market id | peak size | minimum visible size | side | pegged reference | volume | offset |
            | party0 | USD/DEC20 | 2         | 1                    | sell | ASK              | 100    | 20     |
            | party0 | USD/DEC20 | 2         | 1                    | buy  | BID              | 100    | 20     |
            | party0 | USD/DEC19 | 2         | 1                    | sell | ASK              | 100    | 20     |
            | party0 | USD/DEC19 | 2         | 1                    | buy  | BID              | 100    | 20     |
 
        And the parties place the following orders:
            | party  | market id | side | volume | price  | resulting trades | type       | tif     | reference   |
            | party1 | USD/DEC20 | buy  | 1      | 90000  | 0                | TYPE_LIMIT | TIF_GTC | buy-ref-1   |
            | party1 | USD/DEC20 | buy  | 1      | 90000  | 0                | TYPE_LIMIT | TIF_GTC | buy-ref-1   |
            | party1 | USD/DEC20 | buy  | 10     | 100000 | 0                | TYPE_LIMIT | TIF_GTC | buy-ref-2   |
            | party2 | USD/DEC20 | sell | 10     | 100000 | 0                | TYPE_LIMIT | TIF_GTC | sell-ref-3  |
            | party2 | USD/DEC20 | sell | 1      | 101000 | 0                | TYPE_LIMIT | TIF_GTC | sell-ref-1  |
            | party2 | USD/DEC20 | sell | 1      | 110000 | 0                | TYPE_LIMIT | TIF_GTC | sell-ref-2  |
            | party1 | USD/DEC19 | buy  | 1      | 900    | 0                | TYPE_LIMIT | TIF_GTC | buy-ref-1a  |
            | party1 | USD/DEC19 | buy  | 1      | 900    | 0                | TYPE_LIMIT | TIF_GTC | buy-ref-1a  |
            | party1 | USD/DEC19 | buy  | 10     | 1000   | 0                | TYPE_LIMIT | TIF_GTC | buy-ref-2a  |
            | party2 | USD/DEC19 | sell | 10     | 1000   | 0                | TYPE_LIMIT | TIF_GTC | sell-ref-3a |
            | party2 | USD/DEC19 | sell | 1      | 1010   | 0                | TYPE_LIMIT | TIF_GTC | sell-ref-1a |
            | party2 | USD/DEC19 | sell | 1      | 1100   | 0                | TYPE_LIMIT | TIF_GTC | sell-ref-2a |

        When the network moves ahead "2" blocks
        Then the mark price should be "100000" for the market "USD/DEC20"
        And the mark price should be "1000" for the market "USD/DEC19"

    @SLABug
    Scenario: 007: Offsets are calculated in market units, (0070-MKTD-007)

        Given  the parties submit the following liquidity provision:
            | id  | party  | market id | commitment amount | fee   | lp type    |
            | lp1 | party0 | USD/DEC20 | 5000              | 0.001 | submission |
            | lp1 | party0 | USD/DEC20 | 5000              | 0.001 | amendment  |
            | lp2 | party0 | USD/DEC19 | 5000              | 0.001 | submission |
            | lp2 | party0 | USD/DEC19 | 5000              | 0.001 | amendment  |
        And the parties place the following pegged iceberg orders:
            | party  | market id | peak size | minimum visible size | side | pegged reference | volume | offset |
            | party0 | USD/DEC20 | 2         | 1                    | sell | MID              | 100    | 20     |
            | party0 | USD/DEC20 | 2         | 1                    | buy  | MID              | 100    | 20     |
            | party0 | USD/DEC19 | 2         | 1                    | sell | MID              | 100    | 20     |
            | party0 | USD/DEC19 | 2         | 1                    | buy  | MID              | 100    | 20     |

        And the parties place the following orders:
            | party  | market id | side | volume | price  | resulting trades | type       | tif     | reference   |
            | party1 | USD/DEC20 | buy  | 1      | 90000  | 0                | TYPE_LIMIT | TIF_GTC | buy-ref-1   |
            | party1 | USD/DEC20 | buy  | 1      | 90000  | 0                | TYPE_LIMIT | TIF_GTC | buy-ref-1   |
            | party1 | USD/DEC20 | buy  | 10     | 100000 | 0                | TYPE_LIMIT | TIF_GTC | buy-ref-2   |
            | party2 | USD/DEC20 | sell | 10     | 100000 | 0                | TYPE_LIMIT | TIF_GTC | sell-ref-3  |
            | party2 | USD/DEC20 | sell | 1      | 101000 | 0                | TYPE_LIMIT | TIF_GTC | sell-ref-1  |
            | party2 | USD/DEC20 | sell | 1      | 110000 | 0                | TYPE_LIMIT | TIF_GTC | sell-ref-2  |
            | party1 | USD/DEC19 | buy  | 1      | 900    | 0                | TYPE_LIMIT | TIF_GTC | buy-ref-1a  |
            | party1 | USD/DEC19 | buy  | 1      | 900    | 0                | TYPE_LIMIT | TIF_GTC | buy-ref-1a  |
            | party1 | USD/DEC19 | buy  | 10     | 1000   | 0                | TYPE_LIMIT | TIF_GTC | buy-ref-2a  |
            | party2 | USD/DEC19 | sell | 10     | 1000   | 0                | TYPE_LIMIT | TIF_GTC | sell-ref-3a |
            | party2 | USD/DEC19 | sell | 1      | 1010   | 0                | TYPE_LIMIT | TIF_GTC | sell-ref-1a |
            | party2 | USD/DEC19 | sell | 1      | 1100   | 0                | TYPE_LIMIT | TIF_GTC | sell-ref-2a |

        When the network moves ahead "2" blocks
        Then the trading mode should be "TRADING_MODE_CONTINUOUS" for the market "USD/DEC19"
        When the parties place the following pegged orders:
            | party  | market id | side | volume | pegged reference | offset |
            | party1 | USD/DEC19 | sell | 5      | ASK              | 5      |
            | party1 | USD/DEC20 | sell | 5      | ASK              | 5      |
        Then the pegged orders should have the following states:
            | party  | market id | side | volume | reference | offset | price  | status        |
            | party1 | USD/DEC20 | sell | 5      | ASK       | 5      | 101005 | STATUS_ACTIVE |
            | party1 | USD/DEC19 | sell | 5      | ASK       | 5      | 1015   | STATUS_ACTIVE |

    Scenario: 008: Price monitoring bounds are calculated at asset precision but displayed rounded, (0070-MKTD-006)

        Given  the parties submit the following liquidity provision:
            | id  | party  | market id | commitment amount | fee   | lp type    |
            | lp1 | party0 | USD/DEC20 | 1000              | 0.001 | submission |
            | lp1 | party0 | USD/DEC20 | 1000              | 0.001 | amendment  |
            | lp1 | party0 | USD/DEC21 | 1000              | 0.001 | submission |
            | lp1 | party0 | USD/DEC21 | 1000              | 0.001 | amendment  |
            | lp2 | party0 | USD/DEC19 | 1000              | 0.001 | submission |
            | lp2 | party0 | USD/DEC19 | 1000              | 0.001 | amendment  |
        And the parties place the following pegged iceberg orders:
            | party  | market id | peak size | minimum visible size | side | pegged reference | volume | offset |
            | party0 | USD/DEC20 | 2         | 1                    | sell | ASK              | 100    | 20     |
            | party0 | USD/DEC20 | 2         | 1                    | buy  | BID              | 100    | 20     |
            | party0 | USD/DEC21 | 2         | 1                    | sell | ASK              | 100    | 20     |
            | party0 | USD/DEC21 | 2         | 1                    | buy  | BID              | 100    | 20     |
            | party0 | USD/DEC19 | 2         | 1                    | sell | ASK              | 100    | 20     |
            | party0 | USD/DEC19 | 2         | 1                    | buy  | BID              | 100    | 20     |
 
        And the parties place the following orders:
            | party  | market id | side | volume | price  | resulting trades | type       | tif     | reference   |
            | party1 | USD/DEC21 | buy  | 10     | 100000 | 0                | TYPE_LIMIT | TIF_GTC | buy-ref-2   |
            | party2 | USD/DEC21 | sell | 10     | 100000 | 0                | TYPE_LIMIT | TIF_GTC | sell-ref-3  |
            | party1 | USD/DEC20 | buy  | 1000   | 100000 | 0                | TYPE_LIMIT | TIF_GTC | buy-ref-2   |
            | party2 | USD/DEC20 | sell | 1000   | 100000 | 0                | TYPE_LIMIT | TIF_GTC | sell-ref-3  |
            | party1 | USD/DEC19 | buy  | 10     | 1000   | 0                | TYPE_LIMIT | TIF_GTC | buy-ref-2a  |
            | party2 | USD/DEC19 | sell | 10     | 1000   | 0                | TYPE_LIMIT | TIF_GTC | sell-ref-3a |
            | party0 | USD/DEC21 | buy  | 1      | 90000  | 0                | TYPE_LIMIT | TIF_GTC | buy-ref-1   |
            | party0 | USD/DEC21 | sell | 1      | 110000 | 0                | TYPE_LIMIT | TIF_GTC | sell-ref-2  |
            | party0 | USD/DEC20 | buy  | 1      | 90000  | 0                | TYPE_LIMIT | TIF_GTC | buy-ref-1   |
            | party0 | USD/DEC20 | sell | 1      | 110000 | 0                | TYPE_LIMIT | TIF_GTC | sell-ref-2  |
            | party0 | USD/DEC19 | buy  | 1      | 900    | 0                | TYPE_LIMIT | TIF_GTC | buy-ref-1a  |
            | party0 | USD/DEC19 | sell | 1      | 1100   | 0                | TYPE_LIMIT | TIF_GTC | sell-ref-2a |

        When the opening auction period ends for market "USD/DEC20"

        Then the price monitoring bounds for the market "USD/DEC19" should be:
            | min bound | max bound |
            | 1000      | 1000      |
        And the price monitoring bounds for the market "USD/DEC20" should be:
            | min bound | max bound |
            | 99955     | 100045    |
        And the price monitoring bounds for the market "USD/DEC21" should be:
            | min bound | max bound |
            | 99955     | 100045    |
