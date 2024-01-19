package f3

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"github.com/filecoin-project/go-bitfield"
	"sort"
)

type GraniteConfig struct {
	// Initial delay for partial synchrony.
	Delta float64
	// Change to delta in each round after the first.
	DeltaRate float64
}

type VRFer interface {
	VRFTicketSource
	VRFTicketVerifier
}

type Phase uint8

const (
	INITIAL_PHASE Phase = iota
	QUALITY_PHASE
	CONVERGE_PHASE
	PREPARE_PHASE
	COMMIT_PHASE
	DECIDE_PHASE
	TERMINATED_PHASE
)

func (p Phase) String() string {
	switch p {
	case INITIAL_PHASE:
		return "INITIAL"
	case QUALITY_PHASE:
		return "QUALITY"
	case CONVERGE_PHASE:
		return "CONVERGE"
	case PREPARE_PHASE:
		return "PREPARE"
	case COMMIT_PHASE:
		return "COMMIT"
	case DECIDE_PHASE:
		return "DECIDE"
	case TERMINATED_PHASE:
		return "TERMINATED"
	default:
		return "UNKNOWN"
	}
}

const DOMAIN_SEPARATION_TAG = "GPBFT"

// A message in the Granite protocol.
// The same message structure is used for all rounds and phases.
// Note that the message is self-attesting so no separate envelope or signature is needed.
// - The signature field fixes the included sender ID via the implied public key;
// - The signature payload includes all fields a sender can freely choose;
// - The ticket field is a signature of the same public key, so also self-attesting.
type GMessage struct {
	// ID of the sender/signer of this message (a miner actor ID).
	Sender ActorID

	Current SignedMessage
	// VRF ticket for CONVERGE messages (otherwise empty byte array).
	Ticket Ticket
	// Signature by the sender's public key over Instance || Round || Step || Value.
	Signature []byte
<<<<<<< HEAD
<<<<<<< HEAD
<<<<<<< HEAD
<<<<<<< HEAD
<<<<<<< HEAD
	// Justification for this message (some messages must be justified by a strong quorum of messages from some previous step).
	Justification Justification
=======

	Evidence AggEvidence
=======

	Justification Justification
}

// Aggregated list of GossiPBFT messages with the same instance, round and value. Used as evidence for justification of messages
type Justification struct {
	Instance uint32

	Round uint32

	Step string

	Value ECChain

	// Indexes in the base power table of the signers (bitset)
	Signers bitfield.BitField
	// BLS aggregate signature of signers
	Signature []byte
}

<<<<<<< HEAD
func (a Justification) isZero() bool {
	signersCount, err := a.Signers.Count()
	if err != nil {
		panic(err)
	}
	return a.Step == "" && a.Value.IsZero() && a.Instance == 0 && a.Round == 0 && signersCount == 0 && len(a.Signature) == 0
>>>>>>> bf3fd83 (Implement aggregation and verification of COMMIT and CONVERGE)
}

// Aggregated list of GossiPBFT messages with the same instance, round and value. Used as evidence for justification of messages
type AggEvidence struct {
	Instance uint32

	Round uint32

	Step string

	Value ECChain

	// Indexes in the base power table of the signers (bitset)
	Signers bitfield.BitField
	// BLS aggregate signature of signers
	Signature []byte
}

func (a AggEvidence) isZero() bool {
	signersCount, err := a.Signers.Count()
	if err != nil {
		panic(err)
	}
	return a.Step == "" && a.Value.IsZero() && a.Instance == 0 && a.Round == 0 && signersCount == 0 && len(a.Signature) == 0
>>>>>>> bcf86d8 (Add AggEvidence type)
=======
>>>>>>> dbf1545 (Merge and address comments)
}

type Justification struct {
	Payload         SignedMessage
	QuorumSignature QuorumSignature
}

// Fields of the message that make up the signature payload.
type SignedMessage struct {
	// GossiPBFT instance (epoch) number.
	Instance uint64
	// GossiPBFT round number.
	Round uint64
	// GossiPBFT step name.
	Step Phase
	// Chain of tipsets proposed/voted for finalisation.
	// Always non-empty; the first entry is the base tipset finalised in the previous instance.
	Value ECChain
}

// Aggregated list of GossiPBFT messages with the same instance, round and value. Used as evidence for justification of messages
type QuorumSignature struct {
=======

	Evidence AggEvidence
}

// Aggregated list of GossiPBFT messages with the same instance, round and value. Used as evidence for justification of messages
type AggEvidence struct {
	Instance uint32

	Round uint32

	Step string

	Value ECChain

>>>>>>> f3066c4 (Add AggEvidence type)
	// Indexes in the base power table of the signers (bitset)
	Signers bitfield.BitField
	// BLS aggregate signature of signers
	Signature []byte
<<<<<<< HEAD
=======
=======
>>>>>>> 9a3e132 (Address comments)
}

<<<<<<< HEAD
func (a AggEvidence) isZero() bool {
	signersCount, err := a.Signers.Count()
	if err != nil {
		panic(err)
	}
	return a.Step == "" && a.Value.IsZero() && a.Instance == 0 && a.Round == 0 && signersCount == 0 && len(a.Signature) == 0
>>>>>>> f3066c4 (Add AggEvidence type)
}

=======
>>>>>>> 07d5e3b (Make lint happy)
=======
>>>>>>> 8dbc0b6 (Make lint happy)
func (m GMessage) String() string {
	// FIXME This needs value receiver to work, for reasons I cannot figure out.
	return fmt.Sprintf("%s{%d}(%d %s)", m.Current.Step, m.Current.Instance, m.Current.Round, &m.Current.Value)
}

// Computes the payload for a GMessage signature.
func SignaturePayload(instance uint64, round uint64, step Phase, value ECChain) []byte {
	var buf bytes.Buffer
	buf.WriteString(DOMAIN_SEPARATION_TAG)
	_ = binary.Write(&buf, binary.BigEndian, instance)
	_ = binary.Write(&buf, binary.BigEndian, round)
	_ = binary.Write(&buf, binary.BigEndian, []byte(step.String()))
	for _, t := range value {
		_ = binary.Write(&buf, binary.BigEndian, t.Epoch)
		buf.Write(t.CID.Bytes())
		_ = binary.Write(&buf, binary.BigEndian, t.Weight)
	}
	return buf.Bytes()
}

// A single Granite consensus instance.
type instance struct {
	config        GraniteConfig
	host          Host
	vrf           VRFer
	participantID ActorID
	instanceID    uint64
	// The EC chain input to this instance.
	input ECChain
	// The power table for the base chain, used for power in this instance.
	powerTable PowerTable
	// The beacon value from the base chain, used for tickets in this instance.
	beacon []byte
	// Current round number.
	round uint64
	// Current phase in the round.
	phase Phase
	// Time at which the current phase can or must end.
	// For QUALITY, PREPARE, and COMMIT, this is the latest time (the phase can end sooner).
	// For CONVERGE, this is the exact time (the timeout solely defines the phase end).
	phaseTimeout float64
	// This instance's proposal for the current round.
	// This is set after the QUALITY phase, and changes only at the end of a full round.
	proposal ECChain
	// The value to be transmitted at the next phase.
	// This value may change away from the proposal between phases.
	value ECChain
	// Queue of messages to be synchronously processed before returning from top-level call.
	inbox []*GMessage
	// Quality phase state (only for round 0)
	quality *quorumState
	// State for each round of phases.
	// State from prior rounds must be maintained to provide justification for values in subsequent rounds.
	rounds map[uint64]*roundState
	// Acceptable chain
	acceptable ECChain
	// Decision state. Collects DECIDE messages until a decision can be made, independently of protocol phases/rounds.
	decision *quorumState
}

func newInstance(
	config GraniteConfig,
	host Host,
	vrf VRFer,
	participantID ActorID,
	instanceID uint64,
	input ECChain,
	powerTable PowerTable,
	beacon []byte) (*instance, error) {
	if input.IsZero() {
		return nil, fmt.Errorf("input is empty")
	}
	return &instance{
		config:        config,
		host:          host,
		vrf:           vrf,
		participantID: participantID,
		instanceID:    instanceID,
		input:         input,
		powerTable:    powerTable,
		beacon:        beacon,
		round:         0,
		phase:         INITIAL_PHASE,
		proposal:      input,
		value:         ECChain{},
		quality:       newQuorumState(powerTable),
		rounds: map[uint64]*roundState{
			0: newRoundState(powerTable),
		},
		acceptable: input,
		decision:   newQuorumState(powerTable),
	}, nil
}

type roundState struct {
	converged *convergeState
	prepared  *quorumState
	committed *quorumState
}

func newRoundState(powerTable PowerTable) *roundState {
	return &roundState{
		converged: newConvergeState(),
		prepared:  newQuorumState(powerTable),
		committed: newQuorumState(powerTable),
	}
}

func (i *instance) Start() error {
	if err := i.beginQuality(); err != nil {
		return err
	}
	return i.drainInbox()
}

// Receives a new acceptable chain and updates its current acceptable chain.
func (i *instance) receiveAcceptable(chain ECChain) {
	i.acceptable = chain
}

func (i *instance) Receive(msg *GMessage) error {
	if i.terminated() {
		return fmt.Errorf("received message after decision")
	}
	if len(i.inbox) > 0 {
		return fmt.Errorf("received message while already processing inbox")
	}

	// Enqueue the message for synchronous processing.
	i.enqueueInbox(msg)
	return i.drainInbox()
}

func (i *instance) ReceiveAlarm(_ string) error {
	if err := i.tryCompletePhase(); err != nil {
		return fmt.Errorf("failed completing protocol phase: %w", err)
	}

	// A phase may have been successfully completed.
	// Re-process any queued messages for the next phase.
	return i.drainInbox()
}

func (i *instance) Describe() string {
	return fmt.Sprintf("P%d{%d}, round %d, phase %s", i.participantID, i.instanceID, i.round, i.phase)
}

func (i *instance) enqueueInbox(msg *GMessage) {
	i.inbox = append(i.inbox, msg)
}

func (i *instance) drainInbox() error {
	for len(i.inbox) > 0 {
		// Process one message.
		// Note the message being processed is left in the inbox until after processing,
		// as a signal that this loop is currently draining the inbox.
		if err := i.receiveOne(i.inbox[0]); err != nil {
			return fmt.Errorf("failed receiving message: %w", err)
		}
		i.inbox = i.inbox[1:]
	}

	return nil
}

// Processes a single message.
func (i *instance) receiveOne(msg *GMessage) error {
	if i.phase == TERMINATED_PHASE {
		return nil // No-op
	}
<<<<<<< HEAD
	round := i.roundState(msg.Current.Round)
=======
	round := i.roundState(msg.Round)
>>>>>>> 6ee56ae (Ensure signing and verifying modifies no input)

	// Drop any messages that can never be valid.
	if !i.isValid(msg) {
		i.log("dropping invalid %s", msg)
		return nil
	}

	if err := i.isJustified(msg); err != nil {
		// No implicit justification:
		// if message not justified explicitly, then it will not be justified
<<<<<<< HEAD
		i.log("dropping unjustified %s from sender %v, error: %s", msg, msg.Sender, err)
=======
		i.log("dropping unjustified %s from sender %v", msg, msg.Sender)
>>>>>>> bf3fd83 (Implement aggregation and verification of COMMIT and CONVERGE)
		return nil
	}

<<<<<<< HEAD
	switch msg.Current.Step {
	case QUALITY_PHASE:
=======
	switch msg.Step {
	case QUALITY:
>>>>>>> 6ee56ae (Ensure signing and verifying modifies no input)
		// Receive each prefix of the proposal independently.
<<<<<<< HEAD
		for j := range msg.Current.Value.Suffix() {
			prefix := msg.Current.Value.Prefix(j + 1)
=======
		for j := range msg.Value.Suffix() {
			prefix := msg.Value.Prefix(j + 1)
>>>>>>> bf3fd83 (Implement aggregation and verification of COMMIT and CONVERGE)
			i.quality.Receive(msg.Sender, prefix, msg.Signature, msg.Justification)
		}
	case CONVERGE_PHASE:
		if err := round.converged.Receive(msg.Current.Value, msg.Ticket); err != nil {
			return fmt.Errorf("failed processing CONVERGE message: %w", err)
		}
<<<<<<< HEAD
	case PREPARE_PHASE:
		round.prepared.Receive(msg.Sender, msg.Current.Value, msg.Signature, msg.Justification)
	case COMMIT_PHASE:
		round.committed.Receive(msg.Sender, msg.Current.Value, msg.Signature, msg.Justification)
	case DECIDE_PHASE:
		i.decision.Receive(msg.Sender, msg.Current.Value, msg.Signature, msg.Justification)
=======
	case PREPARE:
		round.prepared.Receive(msg.Sender, msg.Value, msg.Signature, msg.Justification)
	case COMMIT:
		round.committed.Receive(msg.Sender, msg.Value, msg.Signature, msg.Justification)
	case DECIDE:
		i.decision.Receive(msg.Sender, msg.Value, msg.Signature, msg.Justification)
>>>>>>> bf3fd83 (Implement aggregation and verification of COMMIT and CONVERGE)
	default:
		i.log("unexpected message %v", msg)
	}

	// Try to complete the current phase.
	// Every COMMIT phase stays open to new messages even after the protocol moves on to
	// a new round. Late-arriving COMMITS can still (must) cause a local decision, *in that round*.
<<<<<<< HEAD
	if msg.Current.Step == COMMIT_PHASE && i.phase != DECIDE_PHASE {
		return i.tryCommit(msg.Current.Round)
=======

	if msg.Step == COMMIT && i.phase != DECIDE {
		return i.tryCommit(msg.Round)
>>>>>>> 6ee56ae (Ensure signing and verifying modifies no input)
	}
	return i.tryCompletePhase()
}

// Attempts to complete the current phase and round.
func (i *instance) tryCompletePhase() error {
	i.log("try step %s", i.phase)
	switch i.phase {
	case QUALITY_PHASE:
		return i.tryQuality()
	case CONVERGE_PHASE:
		return i.tryConverge()
	case PREPARE_PHASE:
		return i.tryPrepare()
	case COMMIT_PHASE:
		return i.tryCommit(i.round)
	case DECIDE_PHASE:
		return i.tryDecide()
	case TERMINATED_PHASE:
		return nil // No-op
	default:
		return fmt.Errorf("unexpected phase %s", i.phase)
	}
}

// Checks whether a message is valid.
// An invalid message can never become valid, so may be dropped.
func (i *instance) isValid(msg *GMessage) bool {
	if !i.powerTable.Has(msg.Sender) {
		i.log("sender with zero power or not in power table")
		return false
	}

	_, pubKey := i.powerTable.Get(msg.Sender)

<<<<<<< HEAD
	if !(msg.Current.Value.IsZero() || msg.Current.Value.HasBase(i.input.Base())) {
		i.log("unexpected base %s", &msg.Current.Value)
		return false
	}
	if msg.Current.Step == QUALITY_PHASE {
		return msg.Current.Round == 0 && !msg.Current.Value.IsZero()
	} else if msg.Current.Step == CONVERGE_PHASE {
		if msg.Current.Round == 0 ||
			msg.Current.Value.IsZero() ||
			!i.vrf.VerifyTicket(i.beacon, i.instanceID, msg.Current.Round, pubKey, msg.Ticket) {
=======
	if !(msg.Value.IsZero() || msg.Value.HasBase(i.input.Base())) {
		i.log("unexpected base %s", &msg.Value)
		return false
	}
	if msg.Step == QUALITY {
		return msg.Round == 0 && !msg.Value.IsZero()
	} else if msg.Step == CONVERGE {

		if msg.Round == 0 ||
			msg.Value.IsZero() ||
			!i.vrf.VerifyTicket(i.beacon, i.instanceID, msg.Round, pubKey, msg.Ticket) {
>>>>>>> 6ee56ae (Ensure signing and verifying modifies no input)
			return false
		}
	} else if msg.Current.Step == DECIDE_PHASE {
		// DECIDE needs no justification
		return !msg.Current.Value.IsZero()
	}

<<<<<<< HEAD
	sigPayload := SignaturePayload(msg.Current.Instance, msg.Current.Round, msg.Current.Step, msg.Current.Value)
=======
	sigPayload := SignaturePayload(msg.Instance, msg.Round, msg.Step, msg.Value)
>>>>>>> 6ee56ae (Ensure signing and verifying modifies no input)
	if !i.host.Verify(pubKey, sigPayload, msg.Signature) {
		i.log("invalid signature on %v", msg)
		return false
	}

	return true
}

<<<<<<< HEAD
func (i *instance) VerifyJustification(justification Justification) error {

	power := NewStoragePower(0)
	if err := justification.QuorumSignature.Signers.ForEach(func(bit uint64) error {
		if int(bit) >= len(i.powerTable.Entries) {
			return fmt.Errorf("invalid signer index: %d", bit)
		}
		power.Add(power, i.powerTable.Entries[bit].Power)
		return nil
	}); err != nil {
		return fmt.Errorf("failed to iterate over signers: %w", err)
	}

	if !hasStrongQuorum(power, i.powerTable.Total) {
		return fmt.Errorf("dropping message as no evidence from a strong quorum: %v", justification.QuorumSignature.Signers)
	}

	payload := SignaturePayload(justification.Payload.Instance, justification.Payload.Round, justification.Payload.Step, justification.Payload.Value)
	signers := make([]PubKey, 0)
	if err := justification.QuorumSignature.Signers.ForEach(func(bit uint64) error {
		if int(bit) >= len(i.powerTable.Entries) {
			return fmt.Errorf("invalid signer index: %d", bit)
		}
		signers = append(signers, i.powerTable.Entries[bit].PubKey)
		return nil
	}); err != nil {
		return fmt.Errorf("failed to iterate over signers: %w", err)
	}

	if !i.host.VerifyAggregate(payload, justification.QuorumSignature.Signature, signers) {
		return fmt.Errorf("verification of the aggregate failed: %v", justification)
	}

	return nil
}

func (i *instance) isJustified(msg *GMessage) error {
	switch msg.Current.Step {
	case QUALITY_PHASE, PREPARE_PHASE:
		return nil

	case CONVERGE_PHASE:
		//CONVERGE is justified by a strong quorum of COMMIT for bottom from the previous round.
		// or a strong quorum of PREPARE for the same value from the previous round.

		prevRound := msg.Current.Round - 1
		if msg.Justification.Payload.Round != prevRound {
			return fmt.Errorf("CONVERGE %v has evidence from wrong round %d", msg.Current.Round, msg.Justification.Payload.Round)
		}

		if msg.Justification.Payload.Step == PREPARE_PHASE {
			if msg.Current.Value.HeadCIDOrZero() != msg.Justification.Payload.Value.HeadCIDOrZero() {
				return fmt.Errorf("CONVERGE for value %v has PREPARE evidence for a different value: %v", msg.Current.Value, msg.Justification.Payload.Value)
			}
			if msg.Current.Value.IsZero() {
				return fmt.Errorf("CONVERGE with PREPARE evidence for zero value: %v", msg.Justification.Payload.Value)
			}
		} else if msg.Justification.Payload.Step == COMMIT_PHASE {
			if !msg.Justification.Payload.Value.IsZero() {
				return fmt.Errorf("CONVERGE with COMMIT evidence for non-zero value: %v", msg.Justification.Payload.Value)
			}
		} else {
			return fmt.Errorf("CONVERGE with evidence from wrong step %v", msg.Justification.Payload.Step)
		}

	case COMMIT_PHASE:
		// COMMIT is justified by strong quorum of PREPARE from the same round with the same value.
		// COMMIT for bottom is always justified.

		if msg.Current.Value.IsZero() {
			//TODO make sure justification is default zero?
			return nil
		}

		if msg.Current.Round != msg.Justification.Payload.Round {
			return fmt.Errorf("COMMIT %v has evidence from wrong round %d", msg.Current.Round, msg.Justification.Payload.Round)
		}

		if msg.Justification.Payload.Step != PREPARE_PHASE {
			return fmt.Errorf("COMMIT %v has evidence from wrong step %v", msg.Current.Round, msg.Justification.Payload.Step)
		}

		if msg.Current.Value.Head().CID != msg.Justification.Payload.Value.HeadCIDOrZero() {
			return fmt.Errorf("COMMIT %v has evidence for a different value: %v", msg.Current.Value, msg.Justification.Payload.Value)
		}

	case DECIDE_PHASE:
		// Implement actual justification of DECIDES
		// Example: return fmt.Errorf("DECIDE phase not implemented")
		return nil

	default:
		return fmt.Errorf("unknown message step: %v", msg.Current.Step)
	}

	if msg.Current.Instance != msg.Justification.Payload.Instance {
		return fmt.Errorf("message with instanceID %v has evidence from wrong instanceID: %v", msg.Current.Instance, msg.Justification.Payload.Instance)
	}

	return i.VerifyJustification(msg.Justification)
=======
// Checks whether a message is justified.
func (i *instance) isJustified(msg *GMessage) bool {
	if msg.Step == CONVERGE {
		//CONVERGE is justified by a strong quorum of COMMIT for bottom from the previous round.
		// or a strong quorum of PREPARE for the same value from the previous round.
		prevRound := msg.Round - 1
		if msg.Justification.Round != prevRound {
			i.log("dropping CONVERGE %v with evidence from wrong round %d", msg.Round, msg.Justification.Round)
			return false
		}

		if i.instanceID != msg.Justification.Instance {
			i.log("dropping CONVERGE with instanceID %v with evidence from wrong instanceID: %v", msg.Instance, msg.Justification.Instance)
			return false
		}

		if msg.Justification.Step == PREPARE {
			if msg.Value.HeadCIDOrZero() != msg.Justification.Value.HeadCIDOrZero() {
				i.log("dropping CONVERGE for value %v with PREPARE evidence for a different value: %v", msg.Value, msg.Justification.Value)
				return false
			}
		} else if msg.Justification.Step == COMMIT {
			if msg.Justification.Value.HeadCIDOrZero() != ZeroTipSetID() {
				i.log("dropping CONVERGE with COMMIT evidence for non-zero value: %v", msg.Justification.Value)
				return false
			}
		} else {
			i.log("dropping CONVERGE with evidence from wrong step %v\n", msg.Justification.Step)
			return false
		}

		payload := SignaturePayload(i.instanceID, prevRound, msg.Justification.Step, msg.Justification.Value)
		signers := make([]PubKey, 0)
		if err := msg.Justification.Signers.ForEach(func(bit uint64) error {
			if int(bit) >= len(i.powerTable.Entries) {
				return nil //TODO handle error
			}
			signers = append(signers, i.powerTable.Entries[bit].PubKey)
			return nil
		}); err != nil {
			return false
			//TODO handle error
		}

		if !i.host.VerifyAggregate(payload, msg.Justification.Signature, signers) {
			i.log("dropping CONVERGE %v with invalid evidence signature: %v", msg, msg.Justification)
			return false
		}
	} else if msg.Step == COMMIT {
		// COMMIT is justified by strong quorum of PREPARE from the same round with the same value.
		// COMMIT for bottom is always justified.
		if msg.Value.IsZero() {
			return true
		}
		payload := SignaturePayload(i.instanceID, msg.Round, PREPARE, msg.Value)
		signers := make([]PubKey, 0)
		if err := msg.Justification.Signers.ForEach(func(bit uint64) error {
			if int(bit) >= len(i.powerTable.Entries) {
				return nil //TODO handle error
			}
			signers = append(signers, i.powerTable.Entries[bit].PubKey)
			return nil
		}); err != nil {
			//TODO handle error
			return false
		}
		if !i.host.VerifyAggregate(payload, msg.Justification.Signature, signers) {
			i.log("dropping COMMIT %v with invalid evidence signature: %v", msg, msg.Justification)
			return false
		}
	}
	return true
>>>>>>> bf3fd83 (Implement aggregation and verification of COMMIT and CONVERGE)
}

// Sends this node's QUALITY message and begins the QUALITY phase.
func (i *instance) beginQuality() error {
	if i.phase != INITIAL_PHASE {
		return fmt.Errorf("cannot transition from %s to %s", i.phase, QUALITY_PHASE)
	}
	// Broadcast input value and wait up to Δ to receive from others.
<<<<<<< HEAD
	i.phase = QUALITY_PHASE
	i.phaseTimeout = i.alarmAfterSynchrony(QUALITY_PHASE.String())
	i.broadcast(i.round, QUALITY_PHASE, i.input, nil, Justification{})
	return nil
=======
	i.phase = QUALITY
	i.phaseTimeout = i.alarmAfterSynchrony(QUALITY)
<<<<<<< HEAD
<<<<<<< HEAD
	i.broadcast(i.round, QUALITY, i.input, nil, AggEvidence{})
>>>>>>> 5f43a87 (Require AggEvidence when broadcasting GMessage)
=======
	i.broadcast(i.round, QUALITY, i.input, nil)
>>>>>>> 9a3e132 (Address comments)
=======
	i.broadcast(i.round, QUALITY, i.input, nil, Justification{})
>>>>>>> bf3fd83 (Implement aggregation and verification of COMMIT and CONVERGE)
}

// Attempts to end the QUALITY phase and begin PREPARE based on current state.
func (i *instance) tryQuality() error {
	if i.phase != QUALITY_PHASE {
		return fmt.Errorf("unexpected phase %s, expected %s", i.phase, QUALITY_PHASE)
	}
	// Wait either for a strong quorum that agree on our proposal,
	// or for the timeout to expire.
	foundQuorum := i.quality.HasStrongQuorumAgreement(i.proposal.Head().CID)
	timeoutExpired := i.host.Time() >= i.phaseTimeout

	if foundQuorum {
		// Keep current proposal.
	} else if timeoutExpired {
		strongQuora := i.quality.ListStrongQuorumAgreedValues()
		i.proposal = findFirstPrefixOf(strongQuora, i.proposal)
	}

	if foundQuorum || timeoutExpired {
		i.value = i.proposal
		i.log("adopting proposal/value %s", &i.proposal)
		i.beginPrepare()
	}

	return nil
}

func (i *instance) beginConverge() {
	i.phase = CONVERGE_PHASE
	ticket := i.vrf.MakeTicket(i.beacon, i.instanceID, i.round, i.participantID)
<<<<<<< HEAD
	i.phaseTimeout = i.alarmAfterSynchrony(CONVERGE_PHASE.String())
	prevRoundState := i.roundState(i.round - 1)
	var justification Justification
	var ok bool
	if prevRoundState.committed.HasStrongQuorumAgreement(ZeroTipSetID()) {
		value := ECChain{}
		signers := prevRoundState.committed.getSigners(value)

		signatures := prevRoundState.committed.getSignatures(value, signers)
		aggSignature := make([]byte, 0)
		for _, sig := range signatures {
			aggSignature = i.host.Aggregate([][]byte{sig}, aggSignature)
		}
		justificationPayload := SignedMessage{
			Instance: i.instanceID,
			Round:    i.round - 1,
			Step:     COMMIT_PHASE,
			Value:    value,
		}
		justificationSignature := QuorumSignature{
			Signers:   signers,
			Signature: aggSignature,
		}
		justification = Justification{
			Payload:         justificationPayload,
			QuorumSignature: justificationSignature,
		}
	} else if prevRoundState.prepared.HasStrongQuorumAgreement(i.proposal.Head().CID) {
		value := i.proposal
		signers := prevRoundState.prepared.getSigners(value)
		signatures := prevRoundState.prepared.getSignatures(value, signers)
		aggSignature := make([]byte, 0)
		for _, sig := range signatures {
			aggSignature = i.host.Aggregate([][]byte{sig}, aggSignature)
		}

		justificationPayload := SignedMessage{
			Instance: i.instanceID,
			Round:    i.round - 1,
			Step:     PREPARE_PHASE,
			Value:    value,
		}
		justificationSignature := QuorumSignature{
			Signers:   signers,
			Signature: aggSignature,
		}
		justification = Justification{
			Payload:         justificationPayload,
			QuorumSignature: justificationSignature,
		}
	} else if justification, ok = prevRoundState.committed.justifiedMessages[i.proposal.Head().CID]; ok {
		//justificationPayload already assigned in the if statement
	} else {
		panic("beginConverge called but no evidence found")
	}
	i.broadcast(i.round, CONVERGE_PHASE, i.proposal, ticket, justification)
=======
	i.phaseTimeout = i.alarmAfterSynchrony(CONVERGE)
<<<<<<< HEAD
<<<<<<< HEAD
	i.broadcast(i.round, CONVERGE, i.proposal, ticket, AggEvidence{})
>>>>>>> 5f43a87 (Require AggEvidence when broadcasting GMessage)
=======
	i.broadcast(i.round, CONVERGE, i.proposal, ticket)
>>>>>>> 9a3e132 (Address comments)
=======
	prevRoundState := i.roundState(i.round - 1)
	var justification Justification
	var ok bool
	if prevRoundState.committed.HasStrongQuorumAgreement(ZeroTipSetID()) {
		value := ECChain{}
		signers := prevRoundState.committed.getSigners(value)

		signatures := prevRoundState.committed.getSignatures(value, signers)
		aggSignature := make([]byte, 0)
		for _, sig := range signatures {
			aggSignature = i.host.Aggregate([][]byte{sig}, aggSignature)
		}
		justification = Justification{
			Instance:  i.instanceID,
			Round:     i.round - 1,
			Step:      COMMIT,
			Value:     value,
			Signers:   signers,
			Signature: aggSignature,
		}
	} else if prevRoundState.prepared.HasStrongQuorumAgreement(i.proposal.Head().CID) {
		value := i.proposal
		signers := prevRoundState.prepared.getSigners(value)
		signatures := prevRoundState.prepared.getSignatures(value, signers)
		aggSignature := make([]byte, 0)
		for _, sig := range signatures {
			aggSignature = i.host.Aggregate([][]byte{sig}, aggSignature)
		}

		justification = Justification{
			Instance:  i.instanceID,
			Round:     i.round - 1,
			Step:      PREPARE,
			Value:     value,
			Signers:   signers,
			Signature: aggSignature,
		}
	} else if justification, ok = prevRoundState.committed.justifiedMessages[i.proposal.Head().CID]; ok {
		//justification already assigned in the if statement
	} else {
		panic("beginConverge called but no evidence found")
	}
	i.broadcast(i.round, CONVERGE, i.proposal, ticket, justification)
>>>>>>> bf3fd83 (Implement aggregation and verification of COMMIT and CONVERGE)
}

// Attempts to end the CONVERGE phase and begin PREPARE based on current state.
func (i *instance) tryConverge() error {
	if i.phase != CONVERGE_PHASE {
		return fmt.Errorf("unexpected phase %s, expected %s", i.phase, CONVERGE_PHASE)
	}
	timeoutExpired := i.host.Time() >= i.phaseTimeout
	if !timeoutExpired {
		return nil
	}

	i.value = i.roundState(i.round).converged.findMinTicketProposal()
	if i.value.IsZero() {
		return fmt.Errorf("no values at CONVERGE")
	}
	if i.isAcceptable(i.value) {
		// Sway to proposal if the value is acceptable.
		if !i.proposal.Eq(i.value) {
			i.proposal = i.value
			i.log("adopting proposal %s after converge", &i.proposal)
		}
	} else {
		// Vote for not deciding in this round
		i.value = ECChain{}
	}
	i.beginPrepare()

	return nil
}

// Sends this node's PREPARE message and begins the PREPARE phase.
func (i *instance) beginPrepare() {
	// Broadcast preparation of value and wait for everyone to respond.
<<<<<<< HEAD
	i.phase = PREPARE_PHASE
	i.phaseTimeout = i.alarmAfterSynchrony(PREPARE_PHASE.String())
	i.broadcast(i.round, PREPARE_PHASE, i.value, nil, Justification{})
=======
	i.phase = PREPARE
	i.phaseTimeout = i.alarmAfterSynchrony(PREPARE)
<<<<<<< HEAD
<<<<<<< HEAD
	i.broadcast(i.round, PREPARE, i.value, nil, AggEvidence{})
>>>>>>> 5f43a87 (Require AggEvidence when broadcasting GMessage)
=======
	i.broadcast(i.round, PREPARE, i.value, nil)
>>>>>>> 9a3e132 (Address comments)
=======
	i.broadcast(i.round, PREPARE, i.value, nil, Justification{})
>>>>>>> bf3fd83 (Implement aggregation and verification of COMMIT and CONVERGE)
}

// Attempts to end the PREPARE phase and begin COMMIT based on current state.
func (i *instance) tryPrepare() error {
	if i.phase != PREPARE_PHASE {
		return fmt.Errorf("unexpected phase %s, expected %s", i.phase, PREPARE_PHASE)
	}

	prepared := i.roundState(i.round).prepared
	// Optimisation: we could advance phase once a strong quorum on our proposal is not possible.
	foundQuorum := prepared.HasStrongQuorumAgreement(i.proposal.Head().CID)
	timeoutExpired := i.host.Time() >= i.phaseTimeout

	if foundQuorum {
		i.value = i.proposal
	} else if timeoutExpired {
		i.value = ECChain{}
	}

	if foundQuorum || timeoutExpired {
		i.beginCommit()
	}

	return nil
}

func (i *instance) beginCommit() {
<<<<<<< HEAD
	i.phase = COMMIT_PHASE
	i.phaseTimeout = i.alarmAfterSynchrony(PREPARE_PHASE.String())
	signers := i.roundState(i.round).prepared.getSigners(i.value)
	signatures := i.roundState(i.round).prepared.getSignatures(i.value, signers)
	aggSignature := make([]byte, 0)
	for _, sig := range signatures {
		aggSignature = i.host.Aggregate([][]byte{sig}, aggSignature)
	}
	justificationPayload := SignedMessage{
		Instance: i.instanceID,
		Round:    i.round,
		Step:     PREPARE_PHASE,
		Value:    i.value,
	}
	justificationSignature := QuorumSignature{
		Signers:   signers,
		Signature: aggSignature,
	}
	i.broadcast(i.round, COMMIT_PHASE, i.value, nil, Justification{justificationPayload, justificationSignature})
=======
	i.phase = COMMIT
	i.phaseTimeout = i.alarmAfterSynchrony(PREPARE)
<<<<<<< HEAD
<<<<<<< HEAD
	i.broadcast(i.round, COMMIT, i.value, nil, AggEvidence{})
>>>>>>> 5f43a87 (Require AggEvidence when broadcasting GMessage)
=======
	i.broadcast(i.round, COMMIT, i.value, nil)
>>>>>>> 9a3e132 (Address comments)
=======
	signers := i.roundState(i.round).prepared.getSigners(i.value)
	signatures := i.roundState(i.round).prepared.getSignatures(i.value, signers)
	aggSignature := make([]byte, 0)
	for _, sig := range signatures {
		aggSignature = i.host.Aggregate([][]byte{sig}, aggSignature)
	}
	justification := Justification{
		Instance:  i.instanceID,
		Round:     i.round,
		Step:      PREPARE,
		Value:     i.value,
		Signers:   signers,
		Signature: aggSignature,
	}
	i.broadcast(i.round, COMMIT, i.value, nil, justification)
>>>>>>> bf3fd83 (Implement aggregation and verification of COMMIT and CONVERGE)
}

func (i *instance) tryCommit(round uint64) error {
	// Unlike all other phases, the COMMIT phase stays open to new messages even after an initial quorum is reached,
	// and the algorithm moves on to the next round.
	// A subsequent COMMIT message can cause the node to decide, so there is no check on the current phase.
	committed := i.roundState(round).committed
	foundQuorum := committed.ListStrongQuorumAgreedValues()
	timeoutExpired := i.host.Time() >= i.phaseTimeout

	if len(foundQuorum) > 0 && !foundQuorum[0].IsZero() {
		// A participant may be forced to decide a value that's not its preferred chain.
		// The participant isn't influencing that decision against their interest, just accepting it.
		i.value = foundQuorum[0]
		i.beginDecide()
	} else if i.round == round && i.phase == COMMIT_PHASE && timeoutExpired && committed.ReceivedFromStrongQuorum() {
		// Adopt any non-empty value committed by another participant (there can only be one).
		// This node has observed the strong quorum of PREPARE messages that justify it,
		// and mean that some other nodes may decide that value (if they observe more COMMITs).
		for _, v := range committed.ListAllValues() {
			if !v.IsZero() {
				if !i.isAcceptable(v) {
					i.log("⚠️ swaying from %s to %s by COMMIT", &i.input, &v)
				}
				if !v.Eq(i.proposal) {
					i.proposal = v
					i.log("adopting proposal %s after commit", &i.proposal)
				}
				break
			}
		}

		i.beginNextRound()
	}

	return nil
}

func (i *instance) beginDecide() {
<<<<<<< HEAD
	i.phase = DECIDE_PHASE
	i.broadcast(0, DECIDE_PHASE, i.value, nil, Justification{})
=======
	i.phase = DECIDE
<<<<<<< HEAD
<<<<<<< HEAD
	i.broadcast(0, DECIDE, i.value, nil, AggEvidence{})
>>>>>>> 5f43a87 (Require AggEvidence when broadcasting GMessage)
=======
	i.broadcast(0, DECIDE, i.value, nil)
>>>>>>> 9a3e132 (Address comments)
=======
	i.broadcast(0, DECIDE, i.value, nil, Justification{})
>>>>>>> bf3fd83 (Implement aggregation and verification of COMMIT and CONVERGE)
}

func (i *instance) tryDecide() error {
	foundQuorum := i.decision.ListStrongQuorumAgreedValues()
	if len(foundQuorum) > 0 {
		i.terminate(foundQuorum[0], i.round)
	}

	return nil
}

func (i *instance) roundState(r uint64) *roundState {
	round, ok := i.rounds[r]
	if !ok {
		round = newRoundState(i.powerTable)
		i.rounds[r] = round
	}
	return round
}

func (i *instance) beginNextRound() {
	i.round += 1
	i.log("moving to round %d with %s", i.round, i.proposal.String())
	i.beginConverge()
}

// Returns whether a chain is acceptable as a proposal for this instance to vote for.
// This is "EC Compatible" in the pseudocode.
func (i *instance) isAcceptable(c ECChain) bool {
	return i.acceptable.HasPrefix(c)
}

func (i *instance) terminate(value ECChain, round uint64) {
	i.log("✅ terminated %s in round %d", &i.value, round)
	i.phase = TERMINATED_PHASE
	// Round is a parameter since a late COMMIT message can result in a decision for a round prior to the current one.
	i.round = round
	i.value = value
}

func (i *instance) terminated() bool {
	return i.phase == TERMINATED_PHASE
}

<<<<<<< HEAD
<<<<<<< HEAD
<<<<<<< HEAD
func (i *instance) broadcast(round uint64, step Phase, value ECChain, ticket Ticket, justification Justification) *GMessage {
	payload := SignaturePayload(i.instanceID, round, step, value)
	signature := i.host.Sign(i.participantID, payload)
	sm := SignedMessage{
		Instance: i.instanceID,
		Round:    round,
		Step:     step,
		Value:    value,
	}
	gmsg := &GMessage{i.participantID, sm, ticket, signature, justification}
=======
func (i *instance) broadcast(round uint32, step string, value ECChain, ticket Ticket, evidence AggEvidence) *GMessage {
	payload := SignaturePayload(i.instanceID, round, step, value)
	signature := i.host.Sign(i.participantID, payload)
	gmsg := &GMessage{i.participantID, i.instanceID, round, step, value, ticket, signature, evidence}
>>>>>>> 5f43a87 (Require AggEvidence when broadcasting GMessage)
=======
func (i *instance) broadcast(round uint32, step string, value ECChain, ticket Ticket) *GMessage {
	payload := SignaturePayload(i.instanceID, round, step, value)
	signature := i.host.Sign(i.participantID, payload)
	gmsg := &GMessage{i.participantID, i.instanceID, round, step, value, ticket, signature}
>>>>>>> 9a3e132 (Address comments)
=======
func (i *instance) broadcast(round uint32, step string, value ECChain, ticket Ticket, justification Justification) *GMessage {
	payload := SignaturePayload(i.instanceID, round, step, value)
	signature := i.host.Sign(i.participantID, payload)
	gmsg := &GMessage{i.participantID, i.instanceID, round, step, value, ticket, signature, justification}
>>>>>>> bf3fd83 (Implement aggregation and verification of COMMIT and CONVERGE)
	i.host.Broadcast(gmsg)
	i.enqueueInbox(gmsg)
	return gmsg
}

// Sets an alarm to be delivered after a synchrony delay.
// The delay duration increases with each round.
// Returns the absolute time at which the alarm will fire.
func (i *instance) alarmAfterSynchrony(payload string) float64 {
	timeout := i.host.Time() + i.config.Delta + (float64(i.round) * i.config.DeltaRate)
	i.host.SetAlarm(i.participantID, payload, timeout)
	return timeout
}

func (i *instance) log(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	i.host.Log("P%d{%d}: %s (round %d, step %s, proposal %s, value %s)", i.participantID, i.instanceID, msg,
		i.round, i.phase, &i.proposal, &i.value)
}

///// Incremental quorum-calculation helper /////

// Accumulates values from a collection of senders and incrementally calculates
// which values have reached a strong quorum of support.
// Supports receiving multiple values from each sender, and hence multiple strong quorum values.
type quorumState struct {
	// CID of each chain received, by sender. Allows detecting and ignoring duplicates.
	received map[ActorID]senderSent
	// The power supporting each chain so far.
	chainSupport map[TipSetID]chainSupport
	// Total power of all distinct senders from which some chain has been received so far.
	sendersTotalPower *StoragePower
	// Table of senders' power.
	powerTable PowerTable
	// justifiedMessages stores the received evidences for each message, indexed by the message's head CID.
	justifiedMessages map[TipSetID]Justification
}

// The set of chain heads from one sender and associated signature, and that sender's power.
type senderSent struct {
	heads map[TipSetID][]byte
	power *StoragePower
}

// A chain value and the total power supporting it
type chainSupport struct {
	chain           ECChain
	power           *StoragePower
	signers         map[ActorID]struct{}
	hasStrongQuorum bool
	hasWeakQuorum   bool
}

// Creates a new, empty quorum state.
func newQuorumState(powerTable PowerTable) *quorumState {
	return &quorumState{
		received:          map[ActorID]senderSent{},
		chainSupport:      map[TipSetID]chainSupport{},
		sendersTotalPower: NewStoragePower(0),
		powerTable:        powerTable,
		justifiedMessages: map[TipSetID]Justification{},
	}
}

// Receives a new chain from a sender.
func (q *quorumState) Receive(sender ActorID, value ECChain, signature []byte, justification Justification) {
	head := value.HeadCIDOrZero()
	fromSender, ok := q.received[sender]
	senderPower, _ := q.powerTable.Get(sender)
<<<<<<< HEAD
<<<<<<< HEAD
	sigCopy := make([]byte, len(signature))
	copy(sigCopy, signature)
=======

>>>>>>> bf3fd83 (Implement aggregation and verification of COMMIT and CONVERGE)
=======
	sigCopy := make([]byte, len(signature))
	copy(sigCopy, signature)
>>>>>>> f944d14 (Update tests and calls to signer/verifier interfaces)
	if ok {
		// Don't double-count the same chain head for a single participant.
		if _, ok := fromSender.heads[head]; ok {
			return
		}
<<<<<<< HEAD
<<<<<<< HEAD
		fromSender.heads[head] = sigCopy
=======
		fromSender.heads[head] = signature
>>>>>>> bf3fd83 (Implement aggregation and verification of COMMIT and CONVERGE)
=======
		fromSender.heads[head] = sigCopy
>>>>>>> f944d14 (Update tests and calls to signer/verifier interfaces)
	} else {
		// Add sender's power to total the first time a value is received from them.
		q.sendersTotalPower.Add(q.sendersTotalPower, senderPower)
		fromSender = senderSent{
			heads: map[TipSetID][]byte{
<<<<<<< HEAD
<<<<<<< HEAD
				head: sigCopy,
=======
				head: signature,
>>>>>>> bf3fd83 (Implement aggregation and verification of COMMIT and CONVERGE)
=======
				head: sigCopy,
>>>>>>> f944d14 (Update tests and calls to signer/verifier interfaces)
			},
			power: senderPower,
		}
	}
	q.received[sender] = fromSender

	candidate := chainSupport{
		chain:           value,
		power:           senderPower,
		signers:         make(map[ActorID]struct{}),
		hasStrongQuorum: false,
		hasWeakQuorum:   false,
	}
	if found, ok := q.chainSupport[head]; ok {
		candidate.power.Add(candidate.power, found.power)
		candidate.signers = found.signers
	}
	candidate.signers[sender] = struct{}{}

	candidate.hasStrongQuorum = hasStrongQuorum(candidate.power, q.powerTable.Total)
	candidate.hasWeakQuorum = hasWeakQuorum(candidate.power, q.powerTable.Total)

<<<<<<< HEAD
<<<<<<< HEAD
	if !value.IsZero() && justification.Payload.Step == PREPARE_PHASE { //only committed roundStates need to store justifications
		q.justifiedMessages[value.Head().CID] = justification
	}
=======
	q.justifiedMessages[value.HeadCIDOrZero()] = justification
>>>>>>> bf3fd83 (Implement aggregation and verification of COMMIT and CONVERGE)
=======
	if !value.IsZero() && justification.Step == "PREPARE" { //only committed roundStates need to store justifications
		q.justifiedMessages[value.Head().CID] = justification
	}
<<<<<<< HEAD

>>>>>>> e8b34ed (Only store justification of COMMIT for non-bottom value)
=======
>>>>>>> f944d14 (Update tests and calls to signer/verifier interfaces)
	q.chainSupport[head] = candidate
}

// Checks whether a value has been received before.
func (q *quorumState) HasReceived(value ECChain) bool {
	_, ok := q.chainSupport[value.HeadCIDOrZero()]
	return ok
}

// getSigners retrieves the signers of the given ECChain.
func (q *quorumState) getSigners(value ECChain) bitfield.BitField {
	head := value.HeadCIDOrZero()
	chainSupport, ok := q.chainSupport[head]
	signers := bitfield.New()
	if !ok {
		return signers
	}

	// Copy each element from the original map
<<<<<<< HEAD
<<<<<<< HEAD
	for key := range chainSupport.signers {
=======
	for key, _ := range chainSupport.signers {
>>>>>>> bf3fd83 (Implement aggregation and verification of COMMIT and CONVERGE)
=======
	for key := range chainSupport.signers {
>>>>>>> 8dbc0b6 (Make lint happy)
		signers.Set(uint64(q.powerTable.Lookup[key]))
	}

	return signers
}

// getSignatures returns the corresponding signatures for a given bitset of signers
func (q *quorumState) getSignatures(value ECChain, signers bitfield.BitField) [][]byte {
	head := value.HeadCIDOrZero()
	signatures := make([][]byte, 0)
<<<<<<< HEAD
	if err := signers.ForEach(func(bit uint64) error {
		if int(bit) >= len(q.powerTable.Entries) {
			return fmt.Errorf("invalid signer index: %d", bit)
		}
		if signature, ok := q.received[q.powerTable.Entries[bit].ID].heads[head]; ok {
=======
	if err := signers.ForEach(func(i uint64) error {
		if signature, ok := q.received[q.powerTable.Entries[i].ID].heads[head]; ok {
>>>>>>> bf3fd83 (Implement aggregation and verification of COMMIT and CONVERGE)
			if len(signature) == 0 {
				panic("signature is 0")
			}
			signatures = append(signatures, signature)
		} else {
<<<<<<< HEAD
			panic("QuorumSignature not found")
		}
		return nil
	}); err != nil {
		panic("Error while iterating over signers")
=======
			panic("Signature not found")
		}
		return nil
	}); err != nil {
<<<<<<< HEAD
		return signatures
>>>>>>> bf3fd83 (Implement aggregation and verification of COMMIT and CONVERGE)
=======
		panic("Error while iterating over signers")
>>>>>>> f944d14 (Update tests and calls to signer/verifier interfaces)
		//TODO handle error
	}
	return signatures
}

// Lists all values that have been received from any sender.
// The order of returned values is not defined.
func (q *quorumState) ListAllValues() []ECChain {
	var chains []ECChain
	for _, cp := range q.chainSupport {
		chains = append(chains, cp.chain)
	}
	return chains
}

// Checks whether at most one distinct value has been received.
func (q *quorumState) HasAgreement() bool {
	return len(q.chainSupport) <= 1
}

// Checks whether at least one message has been received from a strong quorum of senders.
func (q *quorumState) ReceivedFromStrongQuorum() bool {
	return hasStrongQuorum(q.sendersTotalPower, q.powerTable.Total)
}

// Checks whether a chain (head) has reached a strong quorum.
func (q *quorumState) HasStrongQuorumAgreement(cid TipSetID) bool {
	cp, ok := q.chainSupport[cid]
	return ok && cp.hasStrongQuorum
}

// Checks whether a chain (head) has reached weak quorum.
func (q *quorumState) HasWeakQuorumAgreement(cid TipSetID) bool {
	cp, ok := q.chainSupport[cid]
	return ok && cp.hasWeakQuorum
}

// Returns a list of the chains which have reached an agreeing strong quorum.
// The order of returned values is not defined.
func (q *quorumState) ListStrongQuorumAgreedValues() []ECChain {
	var withQuorum []ECChain
	for cid, cp := range q.chainSupport {
		if cp.hasStrongQuorum {
			withQuorum = append(withQuorum, q.chainSupport[cid].chain)
		}
	}
	sortByWeight(withQuorum)
	return withQuorum
}

//// CONVERGE phase helper /////

type convergeState struct {
	// Chains indexed by head CID
	values map[TipSetID]ECChain
	// Tickets provided by proposers of each chain.
	tickets map[TipSetID][]Ticket
}

func newConvergeState() *convergeState {
	return &convergeState{
		values:  map[TipSetID]ECChain{},
		tickets: map[TipSetID][]Ticket{},
	}
}

// Receives a new CONVERGE value from a sender.
func (c *convergeState) Receive(value ECChain, ticket Ticket) error {
	if value.IsZero() {
		return fmt.Errorf("bottom cannot be justified for CONVERGE")
	}
	key := value.Head().CID
	c.values[key] = value
	c.tickets[key] = append(c.tickets[key], ticket)

	return nil
}

func (c *convergeState) findMinTicketProposal() ECChain {
	var minTicket Ticket
	var minValue ECChain
	for cid, value := range c.values {
		for _, ticket := range c.tickets[cid] {
			if minTicket == nil || ticket.Compare(minTicket) < 0 {
				minTicket = ticket
				minValue = value
			}
		}
	}
	return minValue
}

///// General helpers /////

// Returns the first candidate value that is a prefix of the preferred value, or the base of preferred.
func findFirstPrefixOf(candidates []ECChain, preferred ECChain) ECChain {
	for _, v := range candidates {
		if preferred.HasPrefix(v) {
			return v
		}
	}

	// No candidates are a prefix of preferred.
	return preferred.BaseChain()
}

// Sorts chains by weight of their head, descending
func sortByWeight(chains []ECChain) {
	sort.Slice(chains, func(i, j int) bool {
		if chains[i].IsZero() {
			return false
		} else if chains[j].IsZero() {
			return true
		}
		hi := chains[i].Head()
		hj := chains[j].Head()
		return hi.Compare(hj) > 0
	})
}

// Check whether a portion of storage power is a strong quorum of the total
func hasStrongQuorum(part, total *StoragePower) bool {
	two := NewStoragePower(2)
	three := NewStoragePower(3)

	strongThreshold := new(StoragePower).Mul(total, two)
	strongThreshold.Div(strongThreshold, three)
	return part.Cmp(strongThreshold) > 0
}

// Check whether a portion of storage power is a weak quorum of the total
func hasWeakQuorum(part, total *StoragePower) bool {
	three := NewStoragePower(3)

	weakThreshold := new(StoragePower).Div(total, three)
	return part.Cmp(weakThreshold) > 0
}
