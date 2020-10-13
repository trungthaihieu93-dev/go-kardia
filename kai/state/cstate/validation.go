/*
 *  Copyright 2018 KardiaChain
 *  This file is part of the go-kardia library.
 *
 *  The go-kardia library is free software: you can redistribute it and/or modify
 *  it under the terms of the GNU Lesser General Public License as published by
 *  the Free Software Foundation, either version 3 of the License, or
 *  (at your option) any later version.
 *
 *  The go-kardia library is distributed in the hope that it will be useful,
 *  but WITHOUT ANY WARRANTY; without even the implied warranty of
 *  MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
 *  GNU Lesser General Public License for more details.
 *
 *  You should have received a copy of the GNU Lesser General Public License
 *  along with the go-kardia library. If not, see <http://www.gnu.org/licenses/>.
 */

package cstate

import (
	"errors"
	"fmt"
	"time"

	"github.com/kardiachain/go-kardiamain/kai/kaidb"
	"github.com/kardiachain/go-kardiamain/types"
)

func validateBlock(db kaidb.Database, state LastestBlockState, block *types.Block) error {
	// validate internal consistency
	if err := block.ValidateBasic(); err != nil {
		return err
	}

	// validate basic info
	if block.Height() != state.LastBlockHeight+1 {
		return fmt.Errorf("wrong Block.Header.Height. Expected %v, got %v", state.LastBlockHeight+1, block.Height())
	}

	/*	TODO: Determine bounds for Time
		See blockchain/manager "stopSyncingDurationMinutes"
		if !block.Time.After(lastBlockTime) {
			return errors.New("Invalid Block.Header.Time")
		}
	*/

	// validate prev block info
	if !block.Header().LastBlockID.Equal(state.LastBlockID) {
		return fmt.Errorf("Wrong Block.Header.LastBlockID.  Expected %v, got %v", state.LastBlockID, block.Header().LastBlockID)
	}
	// Validate app info
	if !block.AppHash().Equal(state.AppHash) {
		return fmt.Errorf("wrong Block.Header.AppHash.  Expected %X, got %X",
			state.AppHash,
			block.AppHash(),
		)
	}

	if !block.Header().ValidatorsHash.Equal(state.Validators.Hash()) {
		return fmt.Errorf("wrong Block.Header.ValidatorsHash.  Expected %X, got %X",
			state.Validators.Hash(),
			block.Header().ValidatorsHash,
		)
	}

	// TODO(namdoh): Re-enable validating txs
	//newTxs := int64(len(block.Data.Txs))
	//if block.TotalTxs != state.LastBlockTotalTx+newTxs {
	//	return fmt.Errorf("Wrong Block.Header.TotalTxs. Expected %v, got %v", state.LastBlockTotalTx+newTxs, block.TotalTxs)
	//}

	// TODO(namdoh): Re-enable validating other info
	// validate app info
	//if !bytes.Equal(block.AppHash, state.AppHash) {
	//	return fmt.Errorf("Wrong Block.Header.AppHash.  Expected %X, got %v", state.AppHash, block.AppHash)
	//}
	//if !bytes.Equal(block.ConsensusHash, state.ConsensusParams.Hash()) {
	//	return fmt.Errorf("Wrong Block.Header.ConsensusHash.  Expected %X, got %v", state.ConsensusParams.Hash(), block.ConsensusHash)
	//}
	//if !bytes.Equal(block.LastResultsHash, state.LastResultsHash) {
	//	return fmt.Errorf("Wrong Block.Header.LastResultsHash.  Expected %X, got %v", state.LastResultsHash, block.LastResultsHash)
	//}
	//if !bytes.Equal(block.ValidatorsHash, state.Validators.Hash()) {
	//	return fmt.Errorf("Wrong Block.Header.ValidatorsHash.  Expected %X, got %v", state.Validators.Hash(), block.ValidatorsHash)
	//}

	// Validate block LastCommit.
	if block.Header().Height == 1 {
		if len(block.LastCommit().Signatures) != 0 {
			return errors.New("block at height 1 (first block) should have no LastCommit precommits")
		}
	} else {
		if len(block.LastCommit().Signatures) != state.LastValidators.Size() {
			return fmt.Errorf("invalid block commit size. Expected %v, got %v",
				state.LastValidators.Size(), len(block.LastCommit().Signatures))
		}
		err := state.LastValidators.VerifyCommit(
			state.ChainID, state.LastBlockID, uint64(block.Height()-1), block.LastCommit())
		if err != nil {
			return err
		}
	}

	// Limit the amount of evidence
	maxNumEvidence, _ := types.MaxEvidencePerBlock(int64(state.ConsensusParams.Block.MaxBytes))
	numEvidence := int64(len(block.Evidence().Evidence))
	if numEvidence > maxNumEvidence {
		return types.NewErrEvidenceOverflow(maxNumEvidence, numEvidence)

	}

	for _, ev := range block.Evidence().Evidence {
		if err := VerifyEvidence(db, state, ev); err != nil {
			return types.NewErrEvidenceInvalid(ev, err)
		}
	}

	return nil
}

// VerifyEvidence verifies the evidence fully by checking:
// - it is sufficiently recent (MaxAge)
// - it is from a key who was a validator at the given height
// - it is internally consistent
// - it was properly signed by the alleged equivocator
func VerifyEvidence(stateDB kaidb.KeyValueStore, state LastestBlockState, evidence types.Evidence) error {
	var (
		height         = int64(state.LastBlockHeight)
		evidenceParams = state.ConsensusParams.Evidence
		ageNumBlocks   = height - int64(evidence.Height())
		ageDuration    = state.LastBlockTime.Sub(evidence.Time())
	)
	if ageDuration > time.Duration(evidenceParams.MaxAgeDuration) && ageNumBlocks > evidenceParams.MaxAgeNumBlocks {
		return fmt.Errorf(
			"evidence from height %d (created at: %v) is too old; min height is %d and evidence can not be older than %v",
			evidence.Height(),
			evidence.Time(),
			height-evidenceParams.MaxAgeNumBlocks,
			state.LastBlockTime.Add(time.Duration(evidenceParams.MaxAgeDuration)),
		)
	}

	valset, err := LoadValidators(stateDB, evidence.Height())
	if err != nil {
		// TODO: if err is just that we cant find it cuz we pruned, ignore.
		// TODO: if its actually bad evidence, punish peer
		return err
	}

	// The address must have been an active validator at the height.
	// NOTE: we will ignore evidence from H if the key was not a validator
	// at H, even if it is a validator at some nearby H'
	// XXX: this makes lite-client bisection as is unsafe
	// See https://github.com/tendermint/tendermint/issues/3244
	ev := evidence
	height, addr := int64(ev.Height()), ev.Address()
	_, val := valset.GetByAddress(addr)
	if val == nil {
		return fmt.Errorf("address %X was not a validator at height %d", addr, height)
	}

	if err := evidence.Verify(state.ChainID, val.Address); err != nil {
		return err
	}

	return nil
}
