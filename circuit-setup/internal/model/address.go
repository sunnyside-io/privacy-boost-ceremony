package model

import (
	"errors"

	"github.com/testinprod-io/privacy-boost-protocol/pkg/addressprotocol"
)

// AddressShape accepts only dimensions represented by the deployment manifest.
// Root counts are fixed at sixteen; silently dropping a differing dimension
// would authorize a different relation under the same artifact identity.
func (c CircuitSpec) AddressShape() (addressprotocol.Shape, error) {
	if c.AssetFormat != addressprotocol.AssetFormat || c.MaxTrees != 16 {
		return addressprotocol.Shape{}, errors.New("address circuits require their asset format and sixteen note roots")
	}

	auth := c.Type == CircuitTypeEpoch || c.Type == CircuitTypeForced || c.Type == CircuitTypeGiftClaim
	if (auth && c.MaxAuthTrees != 16) || (!auth && c.MaxAuthTrees != 0) {
		return addressprotocol.Shape{}, errors.New("invalid address authorization root count")
	}

	for _, n := range []int{c.BatchSize, c.MaxInputs, c.MaxInPerTx, c.MaxOutPerTx, c.Depth, c.AuthDepth, c.MaxFeeTokens} {
		if n < 0 || n > 65535 {
			return addressprotocol.Shape{}, errors.New("invalid address circuit dimension")
		}
	}

	s := addressprotocol.Shape{
		Family:    addressprotocol.Family(c.Type),
		Batch:     uint32(c.BatchSize),
		Inputs:    uint32(c.MaxInPerTx),
		Outputs:   uint32(c.MaxOutPerTx),
		Depth:     uint32(c.Depth),
		AuthDepth: uint32(c.AuthDepth),
		FeeTokens: uint32(c.MaxFeeTokens),
	}

	// Ceremony configs name forced capacity maxInputs; epoch uses maxInPerTx.
	if c.Type == CircuitTypeForced {
		if c.MaxInPerTx != 0 {
			return s, errors.New("forced circuits use maxInputs")
		}

		s.Inputs = uint32(c.MaxInputs)
	} else if c.MaxInputs != 0 {
		return s, errors.New("unused maxInputs must be zero")
	}

	return s, s.Validate()
}
