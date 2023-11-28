Feature: Test one off transfers

Background:
    Given time is updated to "2021-08-26T00:00:00Z"
    Given the following network parameters are set:
      | name                                    | value |
      | transfer.fee.factor                     |  0.5  |
      | network.markPriceUpdateMaximumFrequency | 0s    |
      | transfer.fee.maxQuantumAmount           |  1    |
      | transfer.feeDiscountDecayFraction       |  0.9    |

    Given the following assets are updated:
    | id    | decimal places | quantum |
    | VEGA  |       0        |   50000 |

    Given the parties deposit on asset's general account the following amount:
    | party    | asset | amount          |
    | f0b40ebdc5b92cf2cf82ff5d0c3f94085d23d5ec2d37d0b929e177c6d4d37e4c   | VEGA  | 10000000        |

    And create the network treasury account for asset "VEGA"

@thing
Scenario: simple successful transfers when (transfer amount * transfer.fee.factor <= transfer.fee.maxQuantumAmount * quantum) (0057-TRAN-001, 0057-TRAN-007, 0057-TRAN-008)



    # make party1 trade to have paid a fee which can be used to offset transfer fees

    Given the parties submit the following one off transfers:
    | id | from   |  from_account_type    |   to   |   to_account_type    | asset | amount | delivery_time         |
    | 1  | f0b40ebdc5b92cf2cf82ff5d0c3f94085d23d5ec2d37d0b929e177c6d4d37e4c |  ACCOUNT_TYPE_GENERAL | a7c4b181ef9bf5e9029a016f854e4ad471208020fd86187d07f0b420004f06a4 | ACCOUNT_TYPE_GENERAL | VEGA  |  10000 | 2021-08-26T00:00:01Z  |
    | 2  | f0b40ebdc5b92cf2cf82ff5d0c3f94085d23d5ec2d37d0b929e177c6d4d37e4c |  ACCOUNT_TYPE_GENERAL | 576380694832d9271682e86fffbbcebc09ca79c259baa5d4d0298e12ecdee303 | ACCOUNT_TYPE_GENERAL | VEGA  |  20000 | 2021-08-26T00:00:02Z  |
    | 3  | f0b40ebdc5b92cf2cf82ff5d0c3f94085d23d5ec2d37d0b929e177c6d4d37e4c |  ACCOUNT_TYPE_GENERAL | a3c024b4e23230c89884a54a813b1ecb4cb0f827a38641c66eeca466da6b2ddf | ACCOUNT_TYPE_GENERAL | VEGA  |  30000 | 2021-08-26T00:00:03Z  |

    Then "f0b40ebdc5b92cf2cf82ff5d0c3f94085d23d5ec2d37d0b929e177c6d4d37e4c" should have general account balance of "9910000" for asset "VEGA"

    Given time is updated to "2021-08-26T00:00:01Z"
    Then "f0b40ebdc5b92cf2cf82ff5d0c3f94085d23d5ec2d37d0b929e177c6d4d37e4c" should have general account balance of "9910000" for asset "VEGA"
    Then "a7c4b181ef9bf5e9029a016f854e4ad471208020fd86187d07f0b420004f06a4" should have general account balance of "10000" for asset "VEGA"

    Given time is updated to "2021-08-26T00:00:02Z"
    Then "f0b40ebdc5b92cf2cf82ff5d0c3f94085d23d5ec2d37d0b929e177c6d4d37e4c" should have general account balance of "9910000" for asset "VEGA"
    Then "a7c4b181ef9bf5e9029a016f854e4ad471208020fd86187d07f0b420004f06a4" should have general account balance of "10000" for asset "VEGA"
    Then "576380694832d9271682e86fffbbcebc09ca79c259baa5d4d0298e12ecdee303" should have general account balance of "20000" for asset "VEGA"

    Given time is updated to "2021-08-26T00:00:03Z"
    Then "f0b40ebdc5b92cf2cf82ff5d0c3f94085d23d5ec2d37d0b929e177c6d4d37e4c" should have general account balance of "9910000" for asset "VEGA"
    Then "a7c4b181ef9bf5e9029a016f854e4ad471208020fd86187d07f0b420004f06a4" should have general account balance of "10000" for asset "VEGA"
    Then "576380694832d9271682e86fffbbcebc09ca79c259baa5d4d0298e12ecdee303" should have general account balance of "20000" for asset "VEGA"
    Then "a3c024b4e23230c89884a54a813b1ecb4cb0f827a38641c66eeca466da6b2ddf" should have general account balance of "30000" for asset "VEGA"


