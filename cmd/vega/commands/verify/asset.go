// Copyright (C) 2023 Gobalsky Labs Limited
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, either version 3 of the
// License, or (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <http://www.gnu.org/licenses/>.

package verify

import (
	"time"

	types "code.vegaprotocol.io/vega/protos/vega"
)

type AssetCmd struct{}

func (opts *AssetCmd) Execute(params []string) error {
	return verifier(params, verifyAsset)
}

func verifyAsset(r *reporter, bs []byte) string {
	prop := &types.Proposal{}
	if !unmarshal(r, bs, prop) {
		return ""
	}

	if len(prop.Reference) <= 0 {
		r.Warn("no proposal.reference specified")
	}

	if len(prop.PartyId) <= 0 {
		r.Err("proposal.partyID is missing")
	} else {
		if !isValidParty(prop.PartyId) {
			r.Warn("proposal.partyID does not seems to be a valid party ID")
		}
	}

	if prop.Terms == nil {
		r.Err("missing proposal.Terms")
	} else {
		verifyAssetTerms(r, prop)
	}

	return marshal(prop)
}

func verifyAssetTerms(r *reporter, prop *types.Proposal) {
	if prop.Terms.ClosingTimestamp == 0 {
		r.Err("prop.terms.closingTimestamp is missing or 0")
	} else if time.Unix(prop.Terms.ClosingTimestamp, 0).Before(time.Now()) {
		r.Warn("prop.terms.closingTimestamp may be in the past")
	}
	if prop.Terms.ValidationTimestamp == 0 {
		r.Err("prop.terms.validationTimestamp is missing or 0")
	} else if time.Unix(prop.Terms.ValidationTimestamp, 0).Before(time.Now()) {
		r.Warn("prop.terms.validationTimestamp may be in the past")
	}

	if prop.Terms.EnactmentTimestamp == 0 {
		r.Err("prop.terms.enactmentTimestamp is missing or 0")
	} else if time.Unix(prop.Terms.EnactmentTimestamp, 0).Before(time.Now()) {
		r.Warn("prop.terms.enactmentTimestamp may be in the past")
	}

	newAsset := prop.Terms.GetNewAsset()
	if newAsset == nil {
		r.Err("prop.terms.newAsset is missing or null")
		return
	}
	if newAsset.Changes == nil {
		r.Err("prop.terms.newAsset.changes is missing or null")
		return
	}

	if len(newAsset.Changes.Name) <= 0 {
		r.Err("prop.terms.newAsset.changes.name is missing or empty")
	}
	if len(newAsset.Changes.Symbol) <= 0 {
		r.Err("prop.terms.newAsset.changes.symbol is missing or empty")
	}
	if newAsset.Changes.Decimals == 0 {
		r.Err("prop.terms.newAsset.changes.decimals is missing or empty")
	}
	if len(newAsset.Changes.Quantum) <= 0 {
		r.Err("prop.terms.newAsset.changes.quantum is missing or empty")
	}

	switch source := newAsset.Changes.Source.(type) {
	case *types.AssetDetails_Erc20:
		contractAddress := source.Erc20.GetContractAddress()
		if len(contractAddress) <= 0 {
			r.Err("prop.terms.newAsset.changes.erc20.contractAddress is missing")
		} else if !isValidEthereumAddress(contractAddress) {
			r.Warn("prop.terms.newAsset.changes.erc20.contractAddress may not be a valid ethereum address")
		}
	default:
		r.Err("unsupported prop.terms.newAsset.changes")
	}
}
