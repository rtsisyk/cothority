package defs

import (
	"bytes"
	"encoding/gob"
	"github.com/dedis/cothority/lib/proof"
	"github.com/dedis/cothority/proto/sign"
)

type MessageType int

type SeqNo byte

const (
	Error MessageType = iota
	StampRequestType
	StampReplyType
	StampClose
)

type StampRequest struct {
	Val []byte // Hash-size value to timestamp
}

// NOTE: In order to decoe correctly the Proof, we need to the get the suite
// somehow. We could just simply add it as a field and not (un)marhsal it
// We'd just make sure that the suite is setup before unmarshaling.
type StampReply struct {
	Sig      []byte                         // Signature on the root
	Prf      proof.Proof                    // Merkle proof of value
	SigBroad sign.SignatureBroadcastMessage // All other elements necessary
}

func (Sreq StampRequest) MarshalBinary() ([]byte, error) {
	var b bytes.Buffer
	enc := gob.NewEncoder(&b)
	err := enc.Encode(Sreq.Val)
	return b.Bytes(), err
}

func (Sreq *StampRequest) UnmarshalBinary(data []byte) error {
	b := bytes.NewBuffer(data)
	dec := gob.NewDecoder(b)
	err := dec.Decode(&Sreq.Val)
	return err
}

func (Srep StampReply) MarshalBinary() ([]byte, error) {
	var b bytes.Buffer
	enc := gob.NewEncoder(&b)
	err := enc.Encode(Srep.Sig)
	err = enc.Encode(Srep.Prf)
	err = enc.Encode(Srep.SigBroad)
	return b.Bytes(), err
}

func (Srep *StampReply) UnmarshalBinary(data []byte) error {
	b := bytes.NewBuffer(data)
	dec := gob.NewDecoder(b)
	err := dec.Decode(&Srep.Sig)
	err = dec.Decode(&Srep.Prf)
	err = dec.Decode(&Srep.SigBroad)
	return err
}

type TimeStampMessage struct {
	ReqNo SeqNo // Request sequence number
	// ErrorReply *ErrorReply // Generic error reply to any request
	Type MessageType
	Sreq *StampRequest
	Srep *StampReply
}

func (tsm TimeStampMessage) MarshalBinary() ([]byte, error) {
	var b bytes.Buffer
	var sub []byte
	var err error
	b.WriteByte(byte(tsm.Type))
	b.WriteByte(byte(tsm.ReqNo))
	// marshal sub message based on its Type
	switch tsm.Type {
	case StampRequestType:
		sub, err = tsm.Sreq.MarshalBinary()
	case StampReplyType:
		sub, err = tsm.Srep.MarshalBinary()
	}
	if err == nil {
		b.Write(sub)
	}
	return b.Bytes(), err
}

func (sm *TimeStampMessage) UnmarshalBinary(data []byte) error {
	sm.Type = MessageType(data[0])
	sm.ReqNo = SeqNo(data[1])
	msgBytes := data[2:]
	var err error
	switch sm.Type {
	case StampRequestType:
		sm.Sreq = &StampRequest{}
		err = sm.Sreq.UnmarshalBinary(msgBytes)
	case StampReplyType:
		sm.Srep = &StampReply{}
		err = sm.Srep.UnmarshalBinary(msgBytes)

	}
	return err
}