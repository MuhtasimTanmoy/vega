package commands_test

import (
	"encoding/hex"
	"testing"

	"code.vegaprotocol.io/vega/commands"
	"code.vegaprotocol.io/vega/libs/crypto"
	commandspb "code.vegaprotocol.io/vega/protos/vega/commands/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

func TestCheckTransaction(t *testing.T) {
	t.Run("Submitting valid transaction succeeds", testSubmittingInvalidSignature)
	t.Run("Submitting valid transaction succeeds", TestSubmittingValidTransactionSucceeds)
	t.Run("Submitting empty transaction fails", testSubmittingEmptyTransactionFails)
	t.Run("Submitting nil transaction fails", testSubmittingNilTransactionFails)
	t.Run("Submitting transaction without version fails", testSubmittingTransactionWithoutVersionFails)
	t.Run("Submitting transaction with unsupported version fails", testSubmittingTransactionWithUnsupportedVersionFails)
	t.Run("Submitting transaction without input data fails", testSubmittingTransactionWithoutInputDataFails)
	t.Run("Submitting transaction without signature fails", testSubmittingTransactionWithoutSignatureFails)
	t.Run("Submitting transaction without signature value fails", testSubmittingTransactionWithoutSignatureValueFails)
	t.Run("Submitting transaction without signature algo fails", testSubmittingTransactionWithoutSignatureAlgoFails)
	t.Run("Submitting transaction without from fails", testSubmittingTransactionWithoutFromFails)
	t.Run("Submitting transaction without public key fails", testSubmittingTransactionWithoutPubKeyFromFails)
	t.Run("Submitting transaction with invalid encoding of value fails", testSubmittingTransactionWithInvalidEncodingOfValueFails)
	t.Run("Submitting transaction with invalid encoding of public key fails", testSubmittingTransactionWithInvalidEncodingOfPubKeyFails)
}

func testSubmittingInvalidSignature(t *testing.T) {
	tx := newValidTransactionV2(t)
	tx.Signature = &commandspb.Signature{
		Value:   crypto.RandomHash(),
		Algo:    "vega/ed25519",
		Version: 1,
	}
	err := checkTransaction(tx)
	require.Error(t, err)
	require.Equal(t, commands.ErrInvalidSignature, err["tx.signature.value"][0])
}

func TestSubmittingValidTransactionSucceeds(t *testing.T) {
	tx := newValidTransactionV2(t)

	err := checkTransaction(tx)

	assert.True(t, err.Empty(), err.Error())
}

func testSubmittingEmptyTransactionFails(t *testing.T) {
	err := checkTransaction(&commandspb.Transaction{})

	assert.Error(t, err)
}

func testSubmittingNilTransactionFails(t *testing.T) {
	err := checkTransaction(nil)

	assert.Contains(t, err.Get("tx"), commands.ErrIsRequired)
}

func testSubmittingTransactionWithoutVersionFails(t *testing.T) {
	tx := newValidTransactionV2(t)
	tx.Version = 0

	err := checkTransaction(tx)

	assert.Contains(t, err.Get("tx.version"), commands.ErrIsRequired)
}

func testSubmittingTransactionWithUnsupportedVersionFails(t *testing.T) {
	tcs := []struct {
		name    string
		version uint32
	}{
		{
			name:    "version 1",
			version: 1,
		}, {
			name:    "version 4",
			version: 4,
		},
	}

	for _, tc := range tcs {
		t.Run(tc.name, func(tt *testing.T) {
			tx := newValidTransactionV2(tt)
			tx.Version = commandspb.TxVersion(tc.version)

			err := checkTransaction(tx)

			assert.Contains(tt, err.Get("tx.version"), commands.ErrIsNotSupported)
		})
	}
}

func testSubmittingTransactionWithoutInputDataFails(t *testing.T) {
	tx := newValidTransactionV2(t)
	tx.InputData = []byte{}

	err := checkTransaction(tx)

	assert.Contains(t, err.Get("tx.input_data"), commands.ErrIsRequired)
}

func testSubmittingTransactionWithoutSignatureFails(t *testing.T) {
	tx := newValidTransactionV2(t)
	tx.Signature = nil

	err := checkTransaction(tx)

	assert.Contains(t, err.Get("tx.signature"), commands.ErrIsRequired)
}

func testSubmittingTransactionWithoutSignatureValueFails(t *testing.T) {
	tx := newValidTransactionV2(t)
	tx.Signature.Value = ""

	err := checkTransaction(tx)

	assert.Contains(t, err.Get("tx.signature.value"), commands.ErrIsRequired)
}

func testSubmittingTransactionWithoutSignatureAlgoFails(t *testing.T) {
	tx := newValidTransactionV2(t)
	tx.Signature.Algo = ""

	err := checkTransaction(tx)

	assert.Contains(t, err.Get("tx.signature.algo"), commands.ErrIsRequired)
}

func testSubmittingTransactionWithoutFromFails(t *testing.T) {
	tx := newValidTransactionV2(t)
	tx.From = nil

	err := checkTransaction(tx)

	assert.Contains(t, err.Get("tx.from"), commands.ErrIsRequired)
}

func testSubmittingTransactionWithoutPubKeyFromFails(t *testing.T) {
	tx := newValidTransactionV2(t)
	tx.From = &commandspb.Transaction_PubKey{
		PubKey: "",
	}

	err := checkTransaction(tx)

	assert.Contains(t, err.Get("tx.from.pub_key"), commands.ErrIsRequired)
}

func testSubmittingTransactionWithInvalidEncodingOfValueFails(t *testing.T) {
	tx := newValidTransactionV2(t)
	tx.Signature.Value = "invalid-hex-encoding"

	err := checkTransaction(tx)

	assert.Contains(t, err.Get("tx.signature.value"), commands.ErrShouldBeHexEncoded, err.Error())
}

func testSubmittingTransactionWithInvalidEncodingOfPubKeyFails(t *testing.T) {
	tx := newValidTransactionV2(t)
	tx.From = &commandspb.Transaction_PubKey{
		PubKey: "my-pub-key",
	}

	err := checkTransaction(tx)

	assert.Contains(t, err.Get("tx.from.pub_key"), commands.ErrShouldBeAValidVegaPubkey)
}

func checkTransaction(cmd *commandspb.Transaction) commands.Errors {
	_, err := commands.CheckTransaction(cmd, "vega-stagnet1-202302211715")

	e, ok := err.(commands.Errors)
	if !ok {
		return commands.NewErrors()
	}

	return e
}

func newValidTransactionV2(t *testing.T) *commandspb.Transaction {
	t.Helper()

	inputData := &commandspb.InputData{
		Nonce:       123456789,
		BlockHeight: 1789,
		Command: &commandspb.InputData_OrderCancellation{
			OrderCancellation: &commandspb.OrderCancellation{
				MarketId: "USD/BTC",
				OrderId:  "7fa6d9f6a9dfa9f66fada",
			},
		},
	}

	rawInputData, err := proto.Marshal(inputData)
	if err != nil {
		t.Fatal(err)
	}

	blah := "08cdf1b5c9071082f605a23f8e01080412406234356235646236326236666163326566666130623638646338326665633934613664376165393930633165303563323237653661333765333636363666626218042240666337666439353630373866623166633964623563313962383866303837346334323939623261373633396164303561343761323863306165663239316235352a0131aa0600"
	rawInputData, err = hex.DecodeString(blah)
	if err != nil {
		panic(err)
	}

	return &commandspb.Transaction{
		InputData: rawInputData,
		Signature: &commandspb.Signature{
			Algo:    "vega/ed25519",
			Value:   "ae7a3b26a207e1908b17612a43bc5c8211aec54b921d226a33661b1a46d79ec681e003e00a0982bfe057f2b78c9c219bf36ec9b288b8d2b7ab3158680efef704",
			Version: 1,
		},
		From: &commandspb.Transaction_PubKey{
			PubKey: "b45b5db62b6fac2effa0b68dc82fec94a6d7ae990c1e05c227e6a37e36666fbb",
		},
		Version: 3,
	}
}
