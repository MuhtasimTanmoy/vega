Feature: Test mark to market settlement

  Background:
    Given the markets:
      | id        | quote name | asset | risk model                  | margin calculator         | auction duration | fees         | price monitoring | data source config     | position decimal places | linear slippage factor | quadratic slippage factor | sla params      |
      | ETH/DEC19 | ETH        | ETH   | default-simple-risk-model-3 | default-margin-calculator | 1                | default-none | default-none     | default-eth-for-future | 3                       | 1e6                    | 1e6                       | default-futures |
    And the following network parameters are set:
      | name                                    | value |
      | market.auction.minimumDuration          | 1     |
      | network.markPriceUpdateMaximumFrequency | 0s    |
      | limits.markets.maxPeggedOrders          | 2     |

  @Liquidation
  Scenario: If settlement amount <= the party’s margin account balance entire settlement amount is transferred from party’s margin account to the market’s temporary settlement account
    Given the parties deposit on asset's general account the following amount:
      | party  | asset | amount |
      | party1 | ETH   | 10000  |
      | party2 | ETH   | 10000  |
      | party3 | ETH   | 10000  |
      | aux    | ETH   | 100000 |
      | aux2   | ETH   | 100000 |
      | lpprov | ETH   | 100000 |

    When the parties submit the following liquidity provision:
      | id  | party  | market id | commitment amount | fee   | lp type    |
      | lp1 | lpprov | ETH/DEC19 | 10000             | 0.001 | submission |
      | lp1 | lpprov | ETH/DEC19 | 10000             | 0.001 | submission |
    And the parties place the following pegged iceberg orders:
      | party  | market id | peak size | minimum visible size | side | pegged reference | volume | offset | reference |
      | lpprov | ETH/DEC19 | 90        | 1                    | buy  | BID              | 90     | 100    | peg-1     |
      | lpprov | ETH/DEC19 | 90        | 1                    | sell | ASK              | 90     | 100    | peg-2     |

    # place auxiliary orders so we always have best bid and best offer as to not trigger the liquidity auction
    When the parties place the following orders:
      | party | market id | side | volume | price | resulting trades | type       | tif     |
      | aux   | ETH/DEC19 | buy  | 1000   | 49    | 0                | TYPE_LIMIT | TIF_GTC |
      | aux   | ETH/DEC19 | sell | 1000   | 5001  | 0                | TYPE_LIMIT | TIF_GTC |
      | aux2  | ETH/DEC19 | buy  | 1000   | 1000  | 0                | TYPE_LIMIT | TIF_GTC |
      | aux   | ETH/DEC19 | sell | 1000   | 1000  | 0                | TYPE_LIMIT | TIF_GTC |

    And the market data for the market "ETH/DEC19" should be:
      | target stake | supplied stake |
      | 1100         | 10000          |
    Then the opening auction period ends for market "ETH/DEC19"
    And the trading mode should be "TRADING_MODE_CONTINUOUS" for the market "ETH/DEC19"
    And the settlement account should have a balance of "0" for the market "ETH/DEC19"

    When the parties place the following orders with ticks:
      | party  | market id | side | volume | price | resulting trades | type       | tif     |
      | party1 | ETH/DEC19 | sell | 1000   | 1000  | 0                | TYPE_LIMIT | TIF_GTC |
      | party2 | ETH/DEC19 | buy  | 1000   | 1000  | 1                | TYPE_LIMIT | TIF_GTC |
    Then the parties should have the following account balances:
      | party  | asset | market id | margin | general |
      | party1 | ETH   | ETH/DEC19 | 4921   | 5079    |
      | party2 | ETH   | ETH/DEC19 | 1273   | 8726    |

    And the settlement account should have a balance of "0" for the market "ETH/DEC19"
    When the parties place the following orders with ticks:
      | party  | market id | side | volume | price | resulting trades | type       | tif     |
      | party1 | ETH/DEC19 | sell | 1000   | 2000  | 0                | TYPE_LIMIT | TIF_GTC |
    Then the parties should have the following account balances:
      | party  | asset | market id | margin | general |
      | party1 | ETH   | ETH/DEC19 | 5041   | 4959    |

    When the parties place the following orders with ticks:
      | party  | market id | side | volume | price | resulting trades | type       | tif     |
      | party3 | ETH/DEC19 | buy  | 1000   | 2000  | 1                | TYPE_LIMIT | TIF_GTC |
    Then the parties should have the following account balances:
      | party  | asset | market id | margin | general |
      | party1 | ETH   | ETH/DEC19 | 0      | 0       |
      | party3 | ETH   | ETH/DEC19 | 2605   | 7393    |
      | party2 | ETH   | ETH/DEC19 | 2605   | 8394    |

    Then the following transfers should happen:
      | from   | to     | from account        | to account              | market id | amount | asset |
      | party1 | market | ACCOUNT_TYPE_MARGIN | ACCOUNT_TYPE_SETTLEMENT | ETH/DEC19 | 1000   | ETH   |
    And the cumulated balance for all accounts should be worth "330000"
    And the settlement account should have a balance of "0" for the market "ETH/DEC19"

  @Liquidation
  Scenario: If settlement amount > party’s margin account balance  and <= party's margin account balance + general account balance for the asset, he full balance of the party’s margin account is transferred to the market’s temporary settlement account the remainder, i.e. difference between the amount transferred from the margin account and the settlement amount, is transferred from the party’s general account for the asset to the market’s temporary settlement account
    Given the parties deposit on asset's general account the following amount:
      | party  | asset | amount       |
      | party1 | ETH   | 10000        |
      | party2 | ETH   | 10000        |
      | party3 | ETH   | 100000       |
      | aux    | ETH   | 100000000000 |
      | aux2   | ETH   | 100000       |
      | lpprov | ETH   | 100000000000 |

    When the parties submit the following liquidity provision:
      | id  | party  | market id | commitment amount | fee   | lp type    |
      | lp1 | lpprov | ETH/DEC19 | 10000             | 0.001 | submission |
      | lp1 | lpprov | ETH/DEC19 | 10000             | 0.001 | submission |
    And the parties place the following pegged iceberg orders:
      | party  | market id | peak size | minimum visible size | side | pegged reference | volume | offset |
      | lpprov | ETH/DEC19 | 40        | 1                    | buy  | BID              | 40     | 100    |
      | lpprov | ETH/DEC19 | 40        | 1                    | sell | ASK              | 40     | 100    |

    # place auxiliary orders so we always have best bid and best offer as to not trigger the liquidity auction
    And the parties place the following orders:
      | party | market id | side | volume | price | resulting trades | type       | tif     |
      | aux   | ETH/DEC19 | buy  | 1000   | 999   | 0                | TYPE_LIMIT | TIF_GTC |
      | aux   | ETH/DEC19 | sell | 1000   | 5001  | 0                | TYPE_LIMIT | TIF_GTC |
      | aux2  | ETH/DEC19 | buy  | 1000   | 1000  | 0                | TYPE_LIMIT | TIF_GTC |
      | aux   | ETH/DEC19 | sell | 1000   | 1000  | 0                | TYPE_LIMIT | TIF_GTC |

    Then the opening auction period ends for market "ETH/DEC19"
    And the trading mode should be "TRADING_MODE_CONTINUOUS" for the market "ETH/DEC19"
    And the settlement account should have a balance of "0" for the market "ETH/DEC19"

    When the parties place the following orders with ticks:
      | party  | market id | side | volume | price | resulting trades | type       | tif     |
      | party1 | ETH/DEC19 | sell | 1000   | 1000  | 0                | TYPE_LIMIT | TIF_GTC |
      | party2 | ETH/DEC19 | buy  | 1000   | 1000  | 1                | TYPE_LIMIT | TIF_GTC |
    Then the parties should have the following account balances:
      | party  | asset | market id | margin | general |
      | party1 | ETH   | ETH/DEC19 | 4921   | 5079    |
      | party2 | ETH   | ETH/DEC19 | 132    | 9867    |

    And the settlement account should have a balance of "0" for the market "ETH/DEC19"
    When the parties place the following orders with ticks:
      | party  | market id | side | volume | price | resulting trades | type       | tif     |
      | party1 | ETH/DEC19 | sell | 1000   | 5000  | 0                | TYPE_LIMIT | TIF_GTC |
    Then the parties should have the following account balances:
      | party  | asset | market id | margin | general |
      | party1 | ETH   | ETH/DEC19 | 5041   | 4959    |

    When the parties place the following orders with ticks:
      | party  | market id | side | volume | price | resulting trades | type       | tif     |
      | party3 | ETH/DEC19 | buy  | 1000   | 5000  | 1                | TYPE_LIMIT | TIF_GTC |
    Then the parties should have the following account balances:
      | party  | asset | market id | margin | general |
      | party1 | ETH   | ETH/DEC19 | 0      | 0       |
      | party3 | ETH   | ETH/DEC19 | 5461   | 94534   |
      | party2 | ETH   | ETH/DEC19 | 5461   | 8538    |
    Then the following transfers should happen:
      | from   | to     | from account        | to account              | market id | amount | asset |
      | party1 | market | ACCOUNT_TYPE_MARGIN | ACCOUNT_TYPE_SETTLEMENT | ETH/DEC19 | 4000   | ETH   |
    And the settlement account should have a balance of "0" for the market "ETH/DEC19"

    # this part show that funds are moved from margin account general account for party 3 as he does not have
    # enough funds in the margin account
    And the settlement account should have a balance of "0" for the market "ETH/DEC19"
    And debug trades
    When the parties place the following orders with ticks:
      | party  | market id | side | volume | price | resulting trades | type       | tif     | reference |
      | party3 | ETH/DEC19 | buy  | 1000   | 50    | 0                | TYPE_LIMIT | TIF_GTC | p3-ref    |
      | party2 | ETH/DEC19 | sell | 1000   | 50    | 1                | TYPE_LIMIT | TIF_GTC | p2-ref    |
    Then the parties should have the following account balances:
      | party  | asset | market id | margin | general |
      | party1 | ETH   | ETH/DEC19 | 0      | 0       |
      | party3 | ETH   | ETH/DEC19 | 1402   | 94592   |
      | party2 | ETH   | ETH/DEC19 | 0      | 9997    |

    Then the following transfers should happen:
      | from   | to     | from account         | to account              | market id | amount | asset |
      | party3 | party3 | ACCOUNT_TYPE_GENERAL | ACCOUNT_TYPE_MARGIN     | ETH/DEC19 | 660    | ETH   |
      | party3 | market | ACCOUNT_TYPE_MARGIN  | ACCOUNT_TYPE_SETTLEMENT | ETH/DEC19 | 4001   | ETH   |
    And the cumulated balance for all accounts should be worth "200000220000"

