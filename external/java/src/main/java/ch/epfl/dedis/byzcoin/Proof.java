package ch.epfl.dedis.byzcoin;

import ch.epfl.dedis.lib.SkipBlock;
import ch.epfl.dedis.lib.SkipblockId;
import ch.epfl.dedis.lib.crypto.Bn256G2Point;
import ch.epfl.dedis.lib.crypto.PointFactory;
import ch.epfl.dedis.lib.darc.DarcId;
import ch.epfl.dedis.lib.exception.CothorityCryptoException;
import ch.epfl.dedis.lib.exception.CothorityException;
import ch.epfl.dedis.lib.exception.CothorityNotFoundException;
import ch.epfl.dedis.lib.network.ServerIdentity;
import ch.epfl.dedis.lib.proto.NetworkProto;
import ch.epfl.dedis.lib.proto.TrieProto;
import ch.epfl.dedis.lib.proto.ByzCoinProto;
import ch.epfl.dedis.lib.proto.SkipchainProto;
import ch.epfl.dedis.skipchain.ForwardLink;
import com.google.protobuf.InvalidProtocolBufferException;

import java.net.URISyntaxException;
import java.nio.ByteBuffer;
import java.nio.ByteOrder;
import java.security.MessageDigest;
import java.security.NoSuchAlgorithmException;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.List;
import java.util.stream.Collectors;

/**
 * Proof represents a key/value entry in the trie and the path to the
 * root node.
 */
public class Proof {
    private TrieProto.Proof proof;
    private List<SkipchainProto.ForwardLink> links;
    private SkipBlock latest;

    private StateChangeBody finalStateChangeBody;

    /**
     * Creates a new proof given a protobuf-representation.
     *
     * @param p the protobuf-representation of the proof
     */
    public Proof(ByzCoinProto.Proof p) throws InvalidProtocolBufferException {
        proof = p.getInclusionproof();
        latest = new SkipBlock(p.getLatest());
        links = p.getLinksList();
        if (!proof.getLeaf().getKey().isEmpty()) {
            finalStateChangeBody = new StateChangeBody(ByzCoinProto.StateChangeBody.parseFrom(proof.getLeaf().getValue()));
        }
    }

    /**
     * @return the instance stored in this proof - it will not verify if the proof is valid!
     * @throws CothorityNotFoundException if the requested instance cannot be found
     */
    public Instance getInstance() throws CothorityNotFoundException{
        return Instance.fromProof(this);
    }

    /**
     * Get the protobuf representation of the proof.
     * @return the protobuf representation of the proof
     */
    public ByzCoinProto.Proof toProto() {
        ByzCoinProto.Proof.Builder b = ByzCoinProto.Proof.newBuilder();
        b.setInclusionproof(proof);
        b.setLatest(latest.getProto());
        for (SkipchainProto.ForwardLink link : this.links) {
            b.addLinks(link);
        }
        return b.build();
    }

    /**
     * accessor for the skipblock assocaited with this proof.
     * @return SkipBlock
     */
    public SkipBlock getLatest() {
        return this.latest;
    }

    /**
     * Verifies the proof with regard to the root id. It will follow all
     * steps and make sure that the hashes work out, starting from the
     * leaf. At the end it will verify against the root hash to make sure
     * that the inclusion proof is correct.
     *
     * @param scID the skipblock to verify
     * @throws CothorityCryptoException if something goes wrong
     */
    public void verify(SkipblockId scID) throws CothorityCryptoException {
        ByzCoinProto.DataHeader header;
        try {
            header = ByzCoinProto.DataHeader.parseFrom(this.latest.getData());
        } catch (InvalidProtocolBufferException e) {
            throw new CothorityCryptoException(e.getMessage());
        }
        if (!Arrays.equals(this.getRoot(), header.getTrieroot().toByteArray())) {
            throw new CothorityCryptoException("root of trie is not in skipblock");
        }

        SkipblockId sbID = null;
        List<Bn256G2Point> publics = null;

        for (int i = 0; i < this.links.size(); i++) {
            if (i == 0) {
                sbID = scID;
                publics = getPoints(this.links.get(i).getNewRoster().getListList());
                continue;
            }
            ForwardLink fl = new ForwardLink(this.links.get(i));
            if (!fl.verify(publics)) {
                throw new CothorityCryptoException("stored skipblock is not properly evolved from genesis block");
            }
            if (!Arrays.equals(fl.getFrom().getId(), sbID.getId())) {
                throw new CothorityCryptoException("stored skipblock is not properly evolved from genesis block");
            }
            sbID = fl.getTo();
            try {
                if (fl.getNewRoster() != null) {
                    publics = getPoints(this.links.get(i).getNewRoster().getListList());
                }
            } catch (URISyntaxException e) {
                throw new CothorityCryptoException(e.getMessage());
            }
        }
    }

    private static List<Bn256G2Point> getPoints(List<NetworkProto.ServerIdentity> protos) throws CothorityCryptoException {
        List<ServerIdentity> sids = new ArrayList<>();
        for (NetworkProto.ServerIdentity sid: protos) {
            try {
                sids.add(new ServerIdentity(sid));
            } catch (URISyntaxException e) {
                throw new CothorityCryptoException(e.getMessage());
            }
        }
        return sids.stream()
                .map(sid -> (Bn256G2Point)sid.getServicePublic("Skipchain"))
                .collect(Collectors.toList());
    }

    /**
     * @return true if the proof has the key/value pair stored, false if it
     * is a proof of absence.
     */
    public boolean matches() {
        // TODO make more verification
        return proof.getLeaf().hasKey() && !proof.getLeaf().getKey().isEmpty();
    }

    public boolean exists(byte[] key) throws CothorityCryptoException {
        if (key == null) {
            throw new CothorityCryptoException("key is nil");
        }

        if (this.proof.getInteriorsCount() == 0) {
            throw new CothorityCryptoException("no interior nodes");
        }

        Boolean[] bits = binSlice(key);
        byte[] expectedHash = hashInterior(this.proof.getInteriors(0));

        int i;
        for (i = 0; i < this.proof.getInteriorsCount(); i++) {
            if (!Arrays.equals(hashInterior(this.proof.getInteriors(i)), expectedHash)) {
                return false;
            }
            if (bits[i]) {
                expectedHash = this.proof.getInteriors(i).getLeft().toByteArray();
            } else {
                expectedHash = this.proof.getInteriors(i).getRight().toByteArray();
            }
        }
        if (Arrays.equals(expectedHash, hashLeaf(this.proof.getLeaf(), this.proof.getNonce().toByteArray()))) {
            if (!Arrays.equals(Arrays.copyOfRange(bits, 0, i+1), this.proof.getLeaf().getPrefixList().toArray())) {
                throw new CothorityCryptoException("invalid prefix in leaf node");
            }
            if (!Arrays.equals(this.proof.getLeaf().getKey().toByteArray(), key)) {
                return false;
            }
            return true;
        } else if (Arrays.equals(expectedHash, hashEmpty(this.proof.getEmpty(), this.proof.getNonce().toByteArray()))) {
            if (!Arrays.equals(Arrays.copyOfRange(bits, 0, i+1), this.proof.getEmpty().getPrefixList().toArray())) {
                throw new CothorityCryptoException("invalid prefix in empty node");
            }
            return false;
        }
        return false;
    }

    private static Boolean[] binSlice(byte[] buf) {
        Boolean[] bits = new Boolean[buf.length*8];
        for (int i = 0; i < bits.length; i++) {
            bits[i] = ((buf[i/8]<<(i%8))&(1<<7)) > 0;
        }
        return bits;
    }

    private static byte[] toByteSlice(List<Boolean> bits) {
        byte[] buf = new byte[(bits.size()+7)/8];
        for (int i = 0; i < bits.size(); i++) {
            if (bits.get(i)) {
                buf[i/8] |= (1 << 7) >> i%8;
            }
        }
        return buf;
    }

    private static byte[] hashInterior(TrieProto.InteriorNode interior) {
        try {
            MessageDigest digest = MessageDigest.getInstance("SHA-256");
            digest.digest(interior.getLeft().toByteArray());
            digest.digest(interior.getRight().toByteArray());
            return digest.digest();
        } catch (NoSuchAlgorithmException e) {
            throw new RuntimeException(e);
        }
    }

    private static byte[] hashLeaf(TrieProto.LeafNode leaf, byte[] nonce) {
        try {
            MessageDigest digest = MessageDigest.getInstance("SHA-256");
            digest.digest(new byte[]{3}); // typeLeaf
            digest.digest(nonce);
            return digest.digest();
        } catch (NoSuchAlgorithmException e) {
            throw new RuntimeException(e);
        }
    }

    private static byte[] hashEmpty(TrieProto.EmptyNode empty, byte[] nonce) {
        try {
            MessageDigest digest = MessageDigest.getInstance("SHA-256");
            digest.digest(new byte[]{2}); // typeLeaf
            digest.digest(nonce);
            digest.digest(toByteSlice(empty.getPrefixList()));

            byte[] lBuf = ByteBuffer.allocate(4).order(ByteOrder.LITTLE_ENDIAN).putInt(empty.getPrefixCount()).array();
            digest.digest(lBuf);
            return digest.digest();
        } catch (NoSuchAlgorithmException e) {
            throw new RuntimeException(e);
        }
    }

    /**
     * @return the key of the leaf node
     */
    public byte[] getKey() {
        return proof.getLeaf().getKey().toByteArray();
    }

    /**
     * @return the list of values in the leaf node.
     */
    public StateChangeBody getValues() {
        return finalStateChangeBody;
    }

    /**
     * @return the value of the proof.
     */
    public byte[] getValue(){
        return getValues().getValue();
    }

    /**
     * @return the string of the contractID.
     */
    public String getContractID(){
        return new String(getValues().getContractID());
    }

    public byte[] getRoot() {
        if (this.proof.getInteriorsCount() == 0) {
            return null;
        }
        return hashInterior(this.proof.getInteriors(0));
    }

    /**
     * @return the darcID defining the access rules to the instance.
     * @throws CothorityCryptoException if there's a problem with the cryptography
     */
    public DarcId getDarcID() throws CothorityCryptoException{
        return getValues().getDarcId();
    }

    /**
     * @param expected the string of the expected contract.
     * @return true if this is a matching byzcoin proof for a contract of the given contract.
     */
    public boolean isContract(String expected){
        if (!getContractID().equals(expected)) {
            return false;
        }
        return true;
    }

    /**
     * Checks if the proof is valid and of type expected.
     *
     * @param expected the expected contractId
     * @param id the Byzcoin id to verify the proof against
     * @return true if the proof is correct with regard to that Byzcoin id and the contract is of the expected type.
     * @throws CothorityException if something goes wrong
     */
    public boolean isContract(String expected, SkipblockId id) throws CothorityException{
        verify(id);
        if (!isContract(expected)){
            return false;
        }
        return true;
    }
}
