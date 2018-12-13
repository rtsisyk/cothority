package personhood

import (
	"errors"

	"github.com/dedis/cothority"
	"github.com/dedis/cothority/byzcoin"
	"github.com/dedis/cothority/darc"
	"github.com/dedis/onet/log"
	"github.com/dedis/onet/network"
	"github.com/dedis/protobuf"
)

// This file holds the contracts for the pop-party. The following contracts
// are defined here:
//   - PopParty - holds the Configuration and later the FinalStatement
//   - PopCoinAccount - represents an account of popcoins

// ContractPopParty represents a pop-party that holds either a configuration
// or a final statement.
var ContractPopParty = "popParty"

type contract struct {
	byzcoin.BasicContract
	PopPartyInstance
}

func contractPopPartyFromBytes(in []byte) (byzcoin.Contract, error) {
	c := &contract{}
	err := protobuf.DecodeWithConstructors(in, &c.PopPartyInstance, network.DefaultConstructors(cothority.Suite))
	if err != nil {
		return nil, errors.New("couldn't unmarshal existing PopPartyInstance: " + err.Error())
	}
	return c, nil
}

func (c *contract) Spawn(rst byzcoin.ReadOnlyStateTrie, inst byzcoin.Instruction, coins []byzcoin.Coin) (scs []byzcoin.StateChange, cout []byzcoin.Coin, err error) {
	cout = coins

	// fsBuf := inst.Spawn.Args.Search("FinalStatement")
	// if fsBuf == nil {
	// 	return nil, nil, errors.New("need FinalStatement argument")
	// }
	// c.State = 1
	//
	// var fs FinalStatement
	// err = protobuf.DecodeWithConstructors(fsBuf, &fs, network.DefaultConstructors(cothority.Suite))
	// if err != nil {
	// 	return nil, nil, errors.New("couldn't unmarshal the final statement: " + err.Error())
	// }
	// c.FinalStatement = &fs

	ppiBuf, err := protobuf.Encode(&c.PopPartyInstance)
	if err != nil {
		return nil, nil, errors.New("couldn't marshal PopPartyInstance: " + err.Error())
	}

	scs = byzcoin.StateChanges{
		byzcoin.NewStateChange(byzcoin.Create, inst.DeriveID(""), inst.Spawn.ContractID, ppiBuf, darc.ID(inst.InstanceID[:])),
	}
	return
}

func (c *contract) Invoke(rst byzcoin.ReadOnlyStateTrie, inst byzcoin.Instruction, coins []byzcoin.Coin) (scs []byzcoin.StateChange, cout []byzcoin.Coin, err error) {
	cout = coins

	var darcID darc.ID
	_, _, _, darcID, err = rst.GetValues(inst.InstanceID.Slice())
	if err != nil {
		return nil, nil, errors.New("couldn't get instance data: " + err.Error())
	}
	log.Print(darcID)

	switch inst.Invoke.Command {
	case "Finalize":
		// if c.State != 1 {
		// 	return nil, nil, fmt.Errorf("can only finalize party with state 1, but current state is %d",
		// 		c.State)
		// }
		// fsBuf := inst.Invoke.Args.Search("FinalStatement")
		// if fsBuf == nil {
		// 	return nil, nil, errors.New("missing argument: FinalStatement")
		// }
		// fs := FinalStatement{}
		// err = protobuf.DecodeWithConstructors(fsBuf, &fs, network.DefaultConstructors(cothority.Suite))
		// if err != nil {
		// 	return nil, nil, errors.New("argument is not a valid FinalStatement")
		// }
		//
		// // TODO: check for aggregate signature of all organizers
		// ppi := PopPartyInstance{
		// 	State:          2,
		// 	FinalStatement: &fs,
		// }
		//
		// for i, pub := range fs.Attendees {
		// 	log.Lvlf3("Creating darc for attendee %d %s", i, pub)
		// 	d, sc, err := createDarc(darcID, pub)
		// 	if err != nil {
		// 		return nil, nil, err
		// 	}
		// 	scs = append(scs, sc)
		//
		// 	sc, err = createCoin(inst, d, pub, 1000000)
		// 	if err != nil {
		// 		return nil, nil, err
		// 	}
		// 	scs = append(scs, sc)
		// }
		//
		// // And add a service if the argument is given
		// sBuf := inst.Invoke.Args.Search("Service")
		// if sBuf != nil {
		// 	ppi.Service = cothority.Suite.Point()
		// 	err = ppi.Service.UnmarshalBinary(sBuf)
		// 	if err != nil {
		// 		return nil, nil, errors.New("couldn't unmarshal point: " + err.Error())
		// 	}
		//
		// 	log.Lvlf3("Checking if service-darc and account for %s should be appended", ppi.Service)
		// 	d, sc, err := createDarc(darcID, ppi.Service)
		// 	if err != nil {
		// 		return nil, nil, err
		// 	}
		// 	_, _, _, _, err = rst.GetValues(d.GetBaseID())
		// 	if err != nil {
		// 		log.Lvl2("Appending service-darc because it doesn't exist yet")
		// 		scs = append(scs, sc)
		// 	}
		//
		// 	log.Lvl3("Creating coin account for service")
		// 	sc, err = createCoin(inst, d, ppi.Service, 0)
		// 	if err != nil {
		// 		return nil, nil, err
		// 	}
		//
		// 	scs = append(scs, sc)
		// }
		//
		// ppiBuf, err := protobuf.Encode(&ppi)
		// if err != nil {
		// 	return nil, nil, errors.New("couldn't marshal PopPartyInstance: " + err.Error())
		// }
		//
		// // Update existing final statement
		// scs = append(scs, byzcoin.NewStateChange(byzcoin.Update, inst.InstanceID, ContractPopParty, ppiBuf, darcID))
		//
		return scs, coins, nil
	case "AddParty":
		return nil, nil, errors.New("not yet implemented")
	default:
		return nil, nil, errors.New("can only finalize Pop-party contract")
	}
}
