Feature: Test loss socialization case 4

  Background:
    Given the markets:
      | id        | quote name | asset | risk model                  | margin calculator         | auction duration | fees         | price monitoring | data source config     | linear slippage factor | quadratic slippage factor | sla params      |
      | ETH/DEC19 | BTC        | BTC   | default-simple-risk-model-2 | default-margin-calculator | 1                | default-none | default-none     | default-eth-for-future | 1e6                    | 1e6                       | default-futures |
    And the following network parameters are set:
      | name                                    | value |
      | market.auction.minimumDuration          | 1     |
      | network.markPriceUpdateMaximumFrequency | 0s    |


  Scenario: Case 4: multiple traders have insufficient MTM & multiple traders socialise the losses (insurance pool partially covers the losses) (0002-STTL-009)
    Description: Case 4 from https://docs.google.com/spreadsheets/d/1CIPH0aQmIKj6YeFW9ApP_l-jwB4OcsNQ/edit#gid=1555964910

    # setup accounts
    Given the initial insurance pool balance is "2900" for all the markets
    Given the parties deposit on asset's general account the following amount:
      | party            | asset | amount    |
      | sellSideProvider | BTC   | 100000000 |
      | buySideProvider  | BTC   | 100000000 |
      | party1           | BTC   | 2000      |
      | party2           | BTC   | 10000     |
      | party3           | BTC   | 3000      |
      | party4           | BTC   | 10000     |
      | party5           | BTC   | 100000000 |
      | party6           | BTC   | 100000000 |
      | aux1             | BTC   | 100000000 |
      | aux2             | BTC   | 100000000 |

    # Trigger an auction to set the mark price
    When the parties place the following orders:
      | party  | market id | side | volume | price | resulting trades | type       | tif     | reference |
      | aux1   | ETH/DEC19 | buy  | 1      | 10    | 0                | TYPE_LIMIT | TIF_GTC | party5-1  |
      | aux2   | ETH/DEC19 | sell | 1      | 1000  | 0                | TYPE_LIMIT | TIF_GTC | party6-1  |
      | party5 | ETH/DEC19 | buy  | 1      | 100   | 0                | TYPE_LIMIT | TIF_GFA | party5-2  |
      | party6 | ETH/DEC19 | sell | 1      | 100   | 0                | TYPE_LIMIT | TIF_GFA | party6-2  |
    Then the opening auction period ends for market "ETH/DEC19"
    And the mark price should be "100" for the market "ETH/DEC19"

    # setup orderbook
    When the parties place the following orders with ticks:
      | party            | market id | side | volume | price | resulting trades | type       | tif     | reference       |
      | sellSideProvider | ETH/DEC19 | sell | 1000   | 120   | 0                | TYPE_LIMIT | TIF_GTC | sell-provider-1 |
      | buySideProvider  | ETH/DEC19 | buy  | 1000   | 80    | 0                | TYPE_LIMIT | TIF_GTC | buy-provider-1  |
    # trade 1 occur
    When the parties place the following orders with ticks:
      | party  | market id | side | volume | price | resulting trades | type       | tif     | reference |
      | party1 | ETH/DEC19 | sell | 30     | 100   | 0                | TYPE_LIMIT | TIF_GTC | ref-1     |
      | party2 | ETH/DEC19 | buy  | 30     | 100   | 1                | TYPE_LIMIT | TIF_GTC | ref-2     |
    # trade 2 occur
    When the parties place the following orders with ticks:
      | party  | market id | side | volume | price | resulting trades | type       | tif     | reference |
      | party3 | ETH/DEC19 | sell | 60     | 100   | 0                | TYPE_LIMIT | TIF_GTC | ref-1     |
      | party2 | ETH/DEC19 | buy  | 60     | 100   | 1                | TYPE_LIMIT | TIF_GTC | ref-2     |
    # trade 3 occur
    When the parties place the following orders with ticks:
      | party  | market id | side | volume | price | resulting trades | type       | tif     | reference |
      | party3 | ETH/DEC19 | sell | 10     | 100   | 0                | TYPE_LIMIT | TIF_GTC | ref-1     |
      | party4 | ETH/DEC19 | buy  | 10     | 100   | 1                | TYPE_LIMIT | TIF_GTC | ref-2     |

    # order book volume change
    Then the parties cancel the following orders:
      | party            | reference       |
      | sellSideProvider | sell-provider-1 |
      | buySideProvider  | buy-provider-1  |
    When the parties place the following orders with ticks:
      | party            | market id | side | volume | price | resulting trades | type       | tif     | reference       |
      | sellSideProvider | ETH/DEC19 | sell | 1000   | 300   | 0                | TYPE_LIMIT | TIF_GTC | sell-provider-2 |
      | buySideProvider  | ETH/DEC19 | buy  | 1000   | 80    | 0                | TYPE_LIMIT | TIF_GTC | buy-provider-2  |
    Then the parties cancel the following orders:
      | party | reference |
      | aux1  | party5-1  |
      | aux2  | party6-1  |

    # trade 4 occur
    When the parties place the following orders with ticks:
      | party  | market id | side | volume | price | resulting trades | type       | tif     | reference |
      | party2 | ETH/DEC19 | buy  | 10     | 180   | 0                | TYPE_LIMIT | TIF_GTC | ref-1     |
      | party4 | ETH/DEC19 | sell | 10     | 180   | 1                | TYPE_LIMIT | TIF_GTC | ref-2     |

    # check positions
    Then the parties should have the following profit and loss:
      | party  | volume | unrealised pnl | realised pnl |
      | party1 | 0      | 0              | -2400        |
      | party2 | 100    | 7200           | -90          |
      | party3 | 0      | 0              | -5500        |
      | party4 | 0      | 0              | 791          |
    And the insurance pool balance should be "0" for the market "ETH/DEC19"
