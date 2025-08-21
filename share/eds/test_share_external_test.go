package eds_test

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/celestiaorg/celestia-app/v5/pkg/wrapper"
	"github.com/celestiaorg/celestia-node/libs/utils"
	"github.com/celestiaorg/celestia-node/share"
	"github.com/celestiaorg/celestia-node/share/eds"
	libshare "github.com/celestiaorg/go-square/v2/share"
	"github.com/celestiaorg/rsmt2d"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBlobFromEDS(t *testing.T) {
	file, err := os.Open("/Users/rootulp/Downloads/1.json")
	require.NoError(t, err)
	defer file.Close()

	data, err := io.ReadAll(file)
	require.NoError(t, err)
	resp := struct {
		Result *rsmt2d.ExtendedDataSquare `json:"result"`
	}{
		Result: &rsmt2d.ExtendedDataSquare{},
	}
	err = json.Unmarshal(data, &resp)
	require.NoError(t, err)
	// Parse namespace from hex string (equivalent to cmdnode.ParseV0Namespace)
	nsParam := "0x72656e6572656e65"
	var nsBytes []byte
	if strings.HasPrefix(nsParam, "0x") {
		nsBytes, err = hex.DecodeString(nsParam[2:])
		require.NoError(t, err)
	}
	ns, err := libshare.NewV0Namespace(nsBytes)
	require.NoError(t, err)

	shrs := resp.Result.FlattenedODS()
	libshares, err := libshare.FromBytes(shrs)
	require.NoError(t, err)
	odsSize := int(utils.SquareSize(len(libshares)))
	square, err := rsmt2d.ComputeExtendedDataSquare(
		libshare.ToBytes(libshares),
		share.DefaultRSMT2DCodec(),
		wrapper.NewConstructor(uint64(odsSize)))
	require.NoError(t, err)

	accessor := &eds.Rsmt2D{ExtendedDataSquare: square}

	nsData, err := eds.NamespaceData(context.Background(), accessor, ns)
	require.NoError(t, err)
	assert.NotNil(t, nsData)
	require.Equal(t, nsData[0].Shares[0].Version(), libshare.ShareVersionOne)
	require.Equal(t, nsData[0].Shares[0].Namespace().Version(), libshare.NamespaceVersionZero)

	fmt.Printf("namespace : %X\n", len(nsData[0].Shares[0].Namespace().ID()))
	fmt.Printf("is sequence start: %v\n", nsData[0].Shares[0].IsSequenceStart())
	fmt.Printf("sequence len: %v\n", nsData[0].Shares[0].SequenceLen())

	flattened := nsData.Flatten()

	assert.False(t, flattened[3422].IsSequenceStart())
	assert.True(t, flattened[3423].IsSequenceStart())

	firstBlobShares := flattened[0:3423]

	libSharesJSON, err := json.Marshal(firstBlobShares)
	require.NoError(t, err)
	exportPath := "/Users/rootulp/git/rootulp/celestiaorg/go-square/share/testdata/mocha-shares.json"
	err = os.WriteFile(exportPath, libSharesJSON, 0644)
	require.NoError(t, err)
	fmt.Printf("Exported %d shares to %s\n", len(firstBlobShares), exportPath)
}
