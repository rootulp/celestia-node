package getters

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/ipfs/go-blockservice"
	"github.com/ipfs/go-cid"
	logging "github.com/ipfs/go-log/v2"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/sync/errgroup"

	"github.com/celestiaorg/nmt"
	"github.com/celestiaorg/nmt/namespace"

	"github.com/celestiaorg/celestia-node/libs/utils"
	"github.com/celestiaorg/celestia-node/share"
	"github.com/celestiaorg/celestia-node/share/ipld"
)

var (
	tracer = otel.Tracer("share/getters")
	log    = logging.Logger("share/getters")

	errOperationNotSupported = errors.New("operation is not supported")
)

// filterRootsByNamespace returns the row roots from the given share.Root that contain the passed
// namespace.
func filterRootsByNamespace(root *share.Root, ns []byte) []cid.Cid {
	rowRootCIDs := make([]cid.Cid, 0, len(root.RowRoots))
	for _, row := range root.RowRoots {
		if isNamespaceInRow(ns, row) {
			rowRootCIDs = append(rowRootCIDs, ipld.MustCidFromNamespacedSha256(row))
		}
	}
	return rowRootCIDs
}

func isNamespaceInRow(ns []byte, rowRoot []byte) bool {
	size := namespace.IDSize(len(ns))
	rowMinNs := nmt.MinNamespace(rowRoot, size)
	rowMaxNs := nmt.MaxNamespace(rowRoot, size)

	return bytes.Compare(rowMinNs, ns) <= 0 && bytes.Compare(ns, rowMaxNs) <= 0
}

// collectSharesByNamespace collects NamespaceShares within the given namespace from the given
// share.Root.
func collectSharesByNamespace(
	ctx context.Context,
	bg blockservice.BlockGetter,
	root *share.Root,
	ns []byte,
) (shares share.NamespacedShares, err error) {
	ctx, span := tracer.Start(ctx, "collect-shares-by-namespace", trace.WithAttributes(
		attribute.String("root", root.String()),
		attribute.String("nid", hex.EncodeToString(ns)),
	))
	defer func() {
		utils.SetStatusAndEnd(span, err)
	}()

	rootCIDs := filterRootsByNamespace(root, ns)
	if len(rootCIDs) == 0 {
		return nil, share.ErrNamespaceNotFound
	}

	errGroup, ctx := errgroup.WithContext(ctx)
	shares = make([]share.NamespacedRow, len(rootCIDs))
	for i, rootCID := range rootCIDs {
		// shadow loop variables, to ensure correct values are captured
		i, rootCID := i, rootCID
		errGroup.Go(func() error {
			row, proof, err := share.GetSharesByNamespace(ctx, bg, rootCID, ns, len(root.RowRoots))
			shares[i] = share.NamespacedRow{
				Shares: row,
				Proof:  proof,
			}
			if err != nil {
				return fmt.Errorf("retrieving nID %x for row %x: %w", ns, rootCID, err)
			}
			return nil
		})
	}

	if err := errGroup.Wait(); err != nil {
		return nil, err
	}

	// return ErrNamespaceNotFound if no shares are found for the namespace.ID
	if len(rootCIDs) == 1 && len(shares[0].Shares) == 0 {
		return nil, share.ErrNamespaceNotFound
	}

	return shares, nil
}

func verifyNamespaceSize(ns []byte) error {
	if size := len(ns); size != share.NamespaceSize {
		return fmt.Errorf("expected namespace of size %d, got %d", share.NamespaceSize, size)
	}
	return nil
}

// ctxWithSplitTimeout will split timeout stored in context by splitFactor and return the result if
// it is greater than minTimeout. minTimeout == 0 will be ignored, splitFactor <= 0 will be ignored
func ctxWithSplitTimeout(
	ctx context.Context,
	splitFactor int,
	minTimeout time.Duration,
) (context.Context, context.CancelFunc) {
	deadline, ok := ctx.Deadline()
	if !ok || splitFactor <= 0 {
		if minTimeout == 0 {
			return context.WithCancel(ctx)
		}
		return context.WithTimeout(ctx, minTimeout)
	}

	timeout := time.Until(deadline)
	if timeout < minTimeout {
		return context.WithCancel(ctx)
	}

	splitTimeout := timeout / time.Duration(splitFactor)
	if splitTimeout < minTimeout {
		return context.WithTimeout(ctx, minTimeout)
	}
	return context.WithTimeout(ctx, splitTimeout)
}

// ErrorContains reports whether any error in err's tree matches any error in targets tree.
func ErrorContains(err, target error) bool {
	if errors.Is(err, target) || target == nil {
		return true
	}

	target = errors.Unwrap(target)
	if target == nil {
		return false
	}
	return ErrorContains(err, target)
}
