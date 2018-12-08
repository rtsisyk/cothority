package personhood

import (
	"crypto/sha256"
	"errors"

	"github.com/dedis/cothority/byzcoin"
	"github.com/dedis/cothority/darc"
	"github.com/dedis/onet/log"
	"github.com/dedis/protobuf"
)

// ContractCredentialID denotes a contract that can spawn new identities.
var ContractCredentialID = "credential"

func contractCredentialFromBytes(in []byte) (byzcoin.Contract, error) {
	c := &contractCredential{}
	err := protobuf.Decode(in, &c.CredentialStruct)
	if err != nil {
		return nil, errors.New("couldn't unmarshal instance data: " + err.Error())
	}
	return c, nil
}

type contractCredential struct {
	byzcoin.BasicContract
	CredentialStruct
}

func (c *contractCredential) Spawn(rst byzcoin.ReadOnlyStateTrie, inst byzcoin.Instruction, coins []byzcoin.Coin) (sc []byzcoin.StateChange, cout []byzcoin.Coin, err error) {
	cout = coins

	var darcID darc.ID
	_, _, _, darcID, err = rst.GetValues(inst.InstanceID.Slice())
	if err != nil {
		return
	}

	// Spawn creates a new coin account as a separate instance.
	ca := inst.DeriveID("")
	log.Lvlf3("Spawning Personhood to %x", ca.Slice())
	var ciBuf []byte
	ciBuf, err = protobuf.Encode(&c.CredentialStruct)
	if err != nil {
		return nil, nil, errors.New("couldn't encode PersonhoodInstance: " + err.Error())
	}
	sc = []byzcoin.StateChange{
		byzcoin.NewStateChange(byzcoin.Create, ca, ContractCredentialID, ciBuf, darcID),
	}
	return
}

func (c *contractCredential) Invoke(rst byzcoin.ReadOnlyStateTrie, inst byzcoin.Instruction, coins []byzcoin.Coin) (sc []byzcoin.StateChange, cout []byzcoin.Coin, err error) {
	cout = coins

	var darcID darc.ID
	_, _, _, darcID, err = rst.GetValues(inst.InstanceID.Slice())
	if err != nil {
		return
	}

	switch inst.Invoke.Command {
	case "transfer":
		// transfer sends a given amount of coins to another account.
		target := inst.Invoke.Args.Search("destination")
		var cid string
		_, _, cid, _, err = rst.GetValues(target)
		if err == nil && cid != ContractCredentialID {
			err = errors.New("destination is not a coin contract")
		}
		if err != nil {
			return
		}

		// sc = append(sc, byzcoin.NewStateChange(byzcoin.Update, byzcoin.NewInstanceID(target),
		// ContractCredentialID, targetBuf, did))
	default:
		err = errors.New("Personhood contract can only")
		return
	}

	// Finally update the coin value.
	var ciBuf []byte
	ciBuf, err = protobuf.Encode(&c.CredentialStruct)
	sc = append(sc, byzcoin.NewStateChange(byzcoin.Update, inst.InstanceID,
		ContractCredentialID, ciBuf, darcID))
	return
}

func (c *contractCredential) Delete(rst byzcoin.ReadOnlyStateTrie, inst byzcoin.Instruction, coins []byzcoin.Coin) (sc []byzcoin.StateChange, cout []byzcoin.Coin, err error) {
	cout = coins

	var darcID darc.ID
	_, _, _, darcID, err = rst.GetValues(inst.InstanceID.Slice())
	if err != nil {
		return
	}

	sc = byzcoin.StateChanges{
		byzcoin.NewStateChange(byzcoin.Remove, inst.InstanceID, ContractCredentialID, nil, darcID),
	}
	return
}

// iid uses sha256(in) in order to manufacture an InstanceID from in
// thereby handling the case where len(in) != 32.
//
// TODO: Find a nicer way to make well-known instance IDs.
func iid(in string) byzcoin.InstanceID {
	h := sha256.New()
	h.Write([]byte(in))
	return byzcoin.NewInstanceID(h.Sum(nil))
}
