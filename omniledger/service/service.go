// Package service implements the lleap service using the collection library to
// handle the merkle-tree. Each call to SetKeyValue updates the Merkle-tree and
// creates a new block containing the root of the Merkle-tree plus the new
// value that has been stored last in the Merkle-tree.
package service

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"sync"
	"time"

	"gopkg.in/dedis/cothority.v2"
	"gopkg.in/dedis/cothority.v2/messaging"
	"gopkg.in/dedis/cothority.v2/skipchain"
	"gopkg.in/dedis/kyber.v2/util/random"
	"gopkg.in/dedis/onet.v2"
	"gopkg.in/dedis/onet.v2/log"
	"gopkg.in/dedis/onet.v2/network"
	"gopkg.in/satori/go.uuid.v1"

	"github.com/dedis/protobuf"
	"github.com/dedis/student_18_omniledger/omniledger/collection"
	"github.com/dedis/student_18_omniledger/omniledger/darc"
)

const darcIDLen int = 32

// Used for tests
// TODO move to test
var omniledgerID onet.ServiceID
var verifyOmniledger = skipchain.VerifierID(uuid.NewV5(uuid.NamespaceURL, "Omniledger"))

func init() {
	var err error
	omniledgerID, err = onet.RegisterNewService(ServiceName, newService)
	log.ErrFatal(err)
	network.RegisterMessages(&storage{}, &DataHeader{}, &updateCollection{})
}

// GenNonce returns a random nonce.
func GenNonce() []byte {
	nonce := make([]byte, 32)
	random.Bytes(nonce, random.New())
	return nonce
}

// Service is our lleap-service
type Service struct {
	// We need to embed the ServiceProcessor, so that incoming messages
	// are correctly handled.
	*onet.ServiceProcessor
	// collections cannot be stored, so they will be re-created whenever the
	// service reloads.
	collectionDB map[string]*collectionDB

	// queueWorkers is a map that points to channels that handle queueing and
	// starting of new blocks.
	queueWorkers map[string]chan ClientTransaction
	// CloseQueues is closed when the queues should stop - this is mostly for
	// testing and there should be a better way to clean up services for testing...
	CloseQueues chan bool
	// classes map kinds to kind specific verification functions
	classes map[string]OmniledgerClass
	// propagate the new transactions
	propagateTransactions messaging.PropagationFunc

	storage *storage

	createSkipChainMut sync.Mutex
}

// storageID reflects the data we're storing - we could store more
// than one structure.
const storageID = "main"

// TODO: this should go into the genesis-configuration
var waitQueueing = 5 * time.Second

// storage is used to save our data locally.
type storage struct {
	sync.Mutex
	// PropTimeout is used when sending the request to integrate a new block
	// to all nodes.
	PropTimeout time.Duration
}

type updateCollection struct {
	ID skipchain.SkipBlockID
}

// CreateGenesisBlock asks the cisc-service to create a new skipchain ready to
// store key/value pairs. If it is given exactly one writer, this writer will
// be stored in the skipchain.
// For faster access, all data is also stored locally in the Service.storage
// structure.
func (s *Service) CreateGenesisBlock(req *CreateGenesisBlock) (
	*CreateGenesisBlockResponse, error) {
	// We use a big mutex here because we do not want to allow concurrent
	// creation of genesis blocks.
	// TODO an optimisation would be to lock on the skipchainID.
	s.createSkipChainMut.Lock()
	defer s.createSkipChainMut.Unlock()

	if req.Version != CurrentVersion {
		return nil, fmt.Errorf("version mismatch - got %d but need %d", req.Version, CurrentVersion)
	}

	darcBuf, err := req.GenesisDarc.ToProto()
	if err != nil {
		return nil, err
	}
	if req.GenesisDarc.Verify() != nil ||
		len(req.GenesisDarc.Rules) == 0 {
		return nil, errors.New("invalid genesis darc")
	}

	// Create the genesis-transaction with a special key, it acts as a
	// reference to the actual genesis transaction.
	transaction := []ClientTransaction{{
		Instructions: []Instruction{{
			DarcID:  req.GenesisDarc.GetID(),
			Nonce:   ZeroNonce,
			Command: "Create",
			Kind:    "config",
			Data:    darcBuf,
		}},
	}}

	sb, err := s.createNewBlock(nil, &req.Roster, transaction)
	if err != nil {
		return nil, err
	}
	s.save()

	s.queueWorkers[string(sb.SkipChainID())], err = s.createQueueWorker(sb.SkipChainID())
	if err != nil {
		return nil, err
	}
	return &CreateGenesisBlockResponse{
		Version:   CurrentVersion,
		Skipblock: sb,
	}, nil
}

// SetKeyValue asks cisc to add a new key/value pair.
func (s *Service) SetKeyValue(req *SetKeyValue) (*SetKeyValueResponse, error) {
	if req.Version != CurrentVersion {
		return nil, errors.New("version mismatch")
	}

	c, ok := s.queueWorkers[string(req.SkipchainID)]
	if !ok {
		return nil, fmt.Errorf("we don't know skipchain ID %x", req.SkipchainID)
	}

	if len(req.Transaction.Instructions) == 0 {
		return nil, errors.New("no transactions to add")
	}

	c <- req.Transaction

	return &SetKeyValueResponse{
		Version: CurrentVersion,
	}, nil
}

// GetProof searches for a key and returns a proof of the
// presence or the absence of this key.
func (s *Service) GetProof(req *GetProof) (resp *GetProofResponse, err error) {
	if req.Version != CurrentVersion {
		return nil, errors.New("version mismatch")
	}
	log.Lvlf2("%s: Getting proof for key %x on sc %x", s.ServerIdentity(), req.Key, req.ID)
	latest, err := s.db().GetLatest(s.db().GetByID(req.ID))
	if err != nil {
		return
	}
	proof, err := NewProof(s.getCollection(req.ID), s.db(), latest.Hash, req.Key)
	if err != nil {
		return
	}
	resp = &GetProofResponse{
		Version: CurrentVersion,
		Proof:   *proof,
	}
	return
}

// SetPropagationTimeout overrides the default propagation timeout that is used
// when a new block is announced to the nodes.
func (s *Service) SetPropagationTimeout(p time.Duration) {
	s.storage.Lock()
	s.storage.PropTimeout = p
	s.storage.Unlock()
}

func padKey(key []byte) []byte {
	keyPadded := make([]byte, 64)
	copy(keyPadded, key)
	return keyPadded
}

func (s *Service) getLatestDarcByID(sid skipchain.SkipBlockID, dID darc.ID) (*darc.Darc, error) {
	colldb := s.getCollection(sid)
	if colldb == nil {
		return nil, fmt.Errorf("collection for skipchain ID %s does not exist", sid.Short())
	}
	value, kind, err := colldb.GetValueKind(padKey(dID))
	if err != nil {
		return nil, err
	}
	if string(kind) != "darc" {
		return nil, fmt.Errorf("for darc %x, expected Kind to be 'darc' but got '%s'", dID, string(kind))
	}
	// TODO we need to make sure this darc is the latest
	return darc.NewDarcFromProto(value)
}

func (s *Service) verifyAndFilterTxs(scID skipchain.SkipBlockID, ts []ClientTransaction) []ClientTransaction {
	var validTxs []ClientTransaction
	for _, t := range ts {
		if err := s.verifyClientTx(scID, t); err != nil {
			log.Error(err)
			continue
		}
		validTxs = append(validTxs, t)
	}
	return validTxs
}

func (s *Service) verifyClientTx(scID skipchain.SkipBlockID, tx ClientTransaction) error {
	for _, instr := range tx.Instructions {
		if err := s.verifyInstruction(scID, instr); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) verifyInstruction(scID skipchain.SkipBlockID, instr Instruction) error {
	d, err := s.getLatestDarcByID(scID, instr.DarcID)
	if err != nil {
		return err
	}
	req, err := instr.ToDarcRequest()
	if err != nil {
		return err
	}
	// TODO we need to use req.VerifyWithCB to search for missing darcs
	return req.Verify(d)
}

// createNewBlock creates a new block and proposes it to the
// skipchain-service. Once the block has been created, we
// inform all nodes to update their internal collections
// to include the new transactions.
func (s *Service) createNewBlock(scID skipchain.SkipBlockID, r *onet.Roster, ts []ClientTransaction) (*skipchain.SkipBlock, error) {
	var sb *skipchain.SkipBlock
	var mr []byte
	var ots []OmniledgerTransaction
	var coll collection.Collection

	if scID.IsNull() {
		// For a genesis block, we create a throwaway collection.
		// There is no need to verify the darc because the caller does
		// it.
		sb = skipchain.NewSkipBlock()
		sb.Roster = r
		sb.MaximumHeight = 10
		sb.BaseHeight = 10
		// We have to register the verification functions in the genesis block
		sb.VerifierIDs = []skipchain.VerifierID{skipchain.VerifyBase, verifyOmniledger}

		coll = collection.New(&collection.Data{}, &collection.Data{})
	} else {
		// For all other blocks, we try to verify the signature using
		// the darcs and remove those that do not have a valid
		// signature before continuing.
		sbLatest, err := s.db().GetLatest(s.db().GetByID(scID))
		if err != nil {
			return nil, errors.New(
				"Could not get latest block from the skipchain: " + err.Error())
		}
		sb = sbLatest.Copy()
		if r != nil {
			sb.Roster = r
		}
		ts = s.verifyAndFilterTxs(sb.SkipChainID(), ts)
		if len(ts) == 0 {
			return nil, errors.New("no valid transaction")
		}
		coll = s.getCollection(scID).coll
	}

	// Note that the transactions are sorted in-place.
	if err := sortTransactions(ts); err != nil {
		return nil, err
	}

	// Create header of skipblock containing only hashes
	mr, ots = s.createOmniledgerTransactions(coll, ts)
	hash := sha256.New()
	for _, tx := range ots {
		txBuf, err := protobuf.Encode(&tx)
		if err != nil {
			log.Lvl2(s.ServerIdentity(), "Couldn't marshal transaction")
		}
		hash.Write(txBuf)
	}
	header := &DataHeader{
		CollectionRoot:  mr,
		TransactionHash: hash.Sum(nil),
		Timestamp:       time.Now().Unix(),
	}
	var err error
	sb.Data, err = network.Marshal(header)
	if err != nil {
		return nil, errors.New("Couldn't marshal data: " + err.Error())
	}

	// Store transactions in the body
	body := &DataBody{Transactions: ots}
	sb.Payload, err = network.Marshal(body)
	if err != nil {
		return nil, errors.New("Couldn't marshal data: " + err.Error())
	}

	var ssb = skipchain.StoreSkipBlock{
		NewBlock:          sb,
		TargetSkipChainID: scID,
	}
	log.Lvlf2("Storing skipblock with transactions %+v", ts)
	ssbReply, err := s.skService().StoreSkipBlock(&ssb)
	if err != nil {
		return nil, err
	}

	s.storage.Lock()
	pto := s.storage.PropTimeout
	s.storage.Unlock()
	// TODO: replace this with some kind of callback from the skipchain-service
	replies, err := s.propagateTransactions(sb.Roster, &updateCollection{sb.Hash}, pto)
	if err != nil {
		log.Lvl1("Propagation-error:", err.Error())
	}
	if replies != len(sb.Roster.List) {
		log.Lvl1(s.ServerIdentity(), "Only got", replies, "out of", len(sb.Roster.List))
	}

	return ssbReply.Latest, nil
}

// updateCollection is called once a skipblock has been stored.
// It is called by the leader, and every node will add the
// transactions in the block to its collection.
func (s *Service) updateCollection(msg network.Message) {
	uc, ok := msg.(*updateCollection)
	if !ok {
		return
	}

	sb, err := s.db().GetLatestByID(uc.ID)
	if err != nil {
		log.Errorf("didn't find latest block for %x", uc.ID)
		return
	}
	_, dataI, err := network.Unmarshal(sb.Data, cothority.Suite)
	data, ok := dataI.(*DataHeader)
	if err != nil || !ok {
		log.Error("couldn't unmarshal header")
		return
	}
	_, bodyI, err := network.Unmarshal(sb.Payload, cothority.Suite)
	body, ok := bodyI.(*DataBody)
	if err != nil || !ok {
		log.Error("couldn't unmarshal body", err, ok)
		return
	}

	log.Lvlf2("%s: Updating transactions for %x", s.ServerIdentity(), sb.SkipChainID())
	cdb := s.getCollection(sb.SkipChainID())
	for _, t := range body.Transactions {
		if !t.Valid {
			continue
		}
		for _, sc := range t.StateChanges {
			log.Lvl2("Storing statechange", sc)
			err = cdb.Store(&sc)
			if err != nil {
				log.Error(
					"error while storing in collection: " + err.Error())
			}
		}
	}
	if !bytes.Equal(cdb.RootHash(), data.CollectionRoot) {
		log.Error("hash of collection doesn't correspond to root hash")
	}
}

func (s *Service) getCollection(id skipchain.SkipBlockID) *collectionDB {
	idStr := fmt.Sprintf("%x", id)
	col := s.collectionDB[idStr]
	if col == nil {
		db, name := s.GetAdditionalBucket([]byte(idStr))
		s.collectionDB[idStr] = newCollectionDB(db, name)
		return s.collectionDB[idStr]
	}
	return col
}

// interface to skipchain.Service
func (s *Service) skService() *skipchain.Service {
	return s.Service(skipchain.ServiceName).(*skipchain.Service)
}

// gives us access to the skipchain's database, so we can get blocks by ID
func (s *Service) db() *skipchain.SkipBlockDB {
	return s.skService().GetDB()
}

// createQueueWorker sets up a worker that will listen on a channel for
// incoming requests and then create a new block every epoch.
func (s *Service) createQueueWorker(scID skipchain.SkipBlockID) (chan ClientTransaction, error) {
	c := make(chan ClientTransaction)
	go func() {
		ts := []ClientTransaction{}
		to := time.After(waitQueueing)
		for {
			select {
			case t := <-c:
				ts = append(ts, t)
				log.Lvlf2("%x: Stored transaction %+v - length is %d: %+v", scID, t, len(ts), ts)
			case <-to:
				log.Lvlf2("%x: New epoch and transaction-length: %d", scID, len(ts))
				if len(ts) > 0 {
					sb, err := s.db().GetLatest(s.db().GetByID(scID))
					if err != nil {
						panic("DB is in bad state and cannot find skipchain anymore: " + err.Error())
					}
					_, err = s.createNewBlock(scID, sb.Roster, ts)
					// We empty ts because createNewBlock only returns an error only if it's a critical failure.
					ts = []ClientTransaction{}
					if err != nil {
						log.Error("couldn't create new block: " + err.Error())
						to = time.After(waitQueueing)
						continue
					}
				}
				to = time.After(waitQueueing)
			case <-s.CloseQueues:
				log.Lvlf2("closing queues...")
				return
			}
		}
	}()
	return c, nil
}

// We use the omniledger as a receiver (as is done in the identity service),
// so we can access e.g. the collectionDBs of the service.
func (s *Service) verifySkipBlock(newID []byte, newSB *skipchain.SkipBlock) bool {
	_, headerI, err := network.Unmarshal(newSB.Data, cothority.Suite)
	header, ok := headerI.(*DataHeader)
	if err != nil || !ok {
		log.Errorf("couldn't unmarshal header")
		return false
	}
	_, bodyI, err := network.Unmarshal(newSB.Payload, cothority.Suite)
	body, ok := bodyI.(*DataBody)
	if err != nil || !ok {
		log.Error("couldn't unmarshal body", err, ok)
		return false
	}

	txs := body.Transactions
	var ctx []ClientTransaction
	for _, t := range txs {
		ctx = append(ctx, t.ClientTransaction)
	}
	cdb := s.getCollection(newSB.Hash)
	mtr, ots := s.createOmniledgerTransactions(cdb.coll, ctx)
	if bytes.Compare(mtr, header.CollectionRoot) != 0 {
		log.Lvl2(s.ServerIdentity(), "Collection root doesn't verify")
		return false
	}
	hash := sha256.New()
	for _, tx := range ots {
		txBuf, err := protobuf.Encode(&tx)
		if err != nil {
			log.Lvl2(s.ServerIdentity(), "Couldn't marshal transaction")
		}
		hash.Write(txBuf)
	}
	if bytes.Compare(hash.Sum(nil), header.TransactionHash) != 0 {
		log.Lvl2(s.ServerIdentity(), "Transaction hash doesn't verify")
		return false
	}
	return true
}

// createOmniledgerTransactions goes through all ClientTransactions and creates
// the appropriate StateChanges and sets the valid flag.
func (s *Service) createOmniledgerTransactions(coll collection.Collection, txs []ClientTransaction) ([]byte, []OmniledgerTransaction) {
	cdbTemp := coll.Clone()
	var otx []OmniledgerTransaction
clientTransactions:
	for _, ctx := range txs {
		cdbI := cdbTemp.Clone()
		ot := OmniledgerTransaction{ClientTransaction: ctx, Valid: true}
		for _, instr := range ctx.Instructions {
			kind, state, err := instr.GetKindState(cdbI)
			if err != nil {
				log.Lvl1("Couldn't get kind of instruction")
				continue clientTransactions
			}

			f, exists := s.classes[kind]
			// If the leader does not have a verifier for this kind, it drops the
			// transaction.
			if !exists {
				log.Lvl1("Leader is dropping instruction of unknown kind:", kind)
				continue clientTransactions
			}
			// Now we call the class function with the data of the key:
			scs, err := f(cdbI, instr, kind, state)
			if err != nil {
				log.Lvl1("Call to class returned error:", err)
				ot.Valid = false
				ot.StateChanges = nil
				cdbI = cdbTemp
				break
			}
			ot.StateChanges = append(ot.StateChanges, scs...)
		}
		otx = append(otx, ot)
		cdbTemp = cdbI
	}
	return cdbTemp.GetRoot(), otx
}

// RegisterVerification stores the verification in a map and will
// call it whenever a verification needs to be done.
func (s *Service) registerVerification(kind string, f OmniledgerClass) error {
	s.classes[kind] = f
	return nil
}

// Tries to load the configuration and updates the data in the service
// if it finds a valid config-file.
func (s *Service) tryLoad() error {
	s.storage = &storage{
		PropTimeout: 10 * time.Second,
	}
	msg, err := s.Load([]byte(storageID))
	if err != nil {
		return err
	}
	if msg != nil {
		var ok bool
		s.storage, ok = msg.(*storage)
		if !ok {
			return errors.New("Data of wrong type")
		}
	}
	if s.storage == nil {
		s.storage = &storage{}
	}
	s.collectionDB = map[string]*collectionDB{}
	s.queueWorkers = map[string]chan ClientTransaction{}

	gas := &skipchain.GetAllSkipchains{}
	gasr, err := s.skService().GetAllSkipchains(gas)
	if err != nil {
		return err
	}
	// GetAllSkipchains erronously returns all skipBLOCKS, so we need
	// to filter out the skipchainIDs.
	scIDs := map[string]bool{}
	for _, sb := range gasr.SkipChains {
		scIDs[string(sb.SkipChainID())] = true
	}

	for scID := range scIDs {
		sbID := skipchain.SkipBlockID(scID)
		s.getCollection(sbID)
		s.queueWorkers[scID], err = s.createQueueWorker(sbID)
		if err != nil {
			return err
		}
	}

	return nil
}

// saves all skipblocks.
func (s *Service) save() {
	s.storage.Lock()
	defer s.storage.Unlock()
	err := s.Save([]byte(storageID), s.storage)
	if err != nil {
		log.Error("Couldn't save file:", err)
	}
}

// newService receives the context that holds information about the node it's
// running on. Saving and loading can be done using the context. The data will
// be stored in memory for tests and simulations, and on disk for real
// deployments.
func newService(c *onet.Context) (onet.Service, error) {
	s := &Service{
		ServiceProcessor: onet.NewServiceProcessor(c),
		CloseQueues:      make(chan bool),
		classes:          make(map[string]OmniledgerClass),
	}
	if err := s.RegisterHandlers(s.CreateGenesisBlock, s.SetKeyValue,
		s.GetProof); err != nil {
		log.ErrFatal(err, "Couldn't register messages")
	}
	if err := s.tryLoad(); err != nil {
		log.Error(err)
		return nil, err
	}

	var err error
	s.propagateTransactions, err = messaging.NewPropagationFunc(c, "OmniledgerPropagate", s.updateCollection, -1)
	if err != nil {
		return nil, err
	}

	s.registerVerification(KindConfig, s.ClassConfig)
	skipchain.RegisterVerification(c, verifyOmniledger, s.verifySkipBlock)
	return s, nil
}
