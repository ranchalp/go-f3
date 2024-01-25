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
	// Justification for this message (some messages must be justified by a strong quorum of messages from some previous step).
	Justification Justification
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
	// Indexes in the base power table of the signers (bitset)
	Signers bitfield.BitField
	// BLS aggregate signature of signers
	Signature []byte
}

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
	round := i.roundState(msg.Current.Round)

	// Drop any messages that can never be valid.
	if !i.isValid(msg) {
		i.log("dropping invalid %s", msg)
		return nil
	}

	if err := i.isJustified(msg); err != nil {
		// No implicit justification:
		// if message not justified explicitly, then it will not be justified
		i.log("dropping unjustified %s from sender %v, error: %s", msg, msg.Sender, err)
		return nil
	}

	switch msg.Current.Step {
	case QUALITY_PHASE:
		// Receive each prefix of the proposal independently.
		for j := range msg.Current.Value.Suffix() {
			prefix := msg.Current.Value.Prefix(j + 1)
			i.quality.Receive(msg.Sender, prefix, msg.Signature, msg.Justification)
		}
	case CONVERGE_PHASE:
		if err := round.converged.Receive(msg.Current.Value, msg.Ticket); err != nil {
			return fmt.Errorf("failed processing CONVERGE message: %w", err)
		}
	case PREPARE_PHASE:
		round.prepared.Receive(msg.Sender, msg.Current.Value, msg.Signature, msg.Justification)
	case COMMIT_PHASE:
		round.committed.Receive(msg.Sender, msg.Current.Value, msg.Signature, msg.Justification)
	case DECIDE_PHASE:
		i.decision.Receive(msg.Sender, msg.Current.Value, msg.Signature, msg.Justification)
	default:
		i.log("unexpected message %v", msg)
	}

	// Try to complete the current phase.
	// Every COMMIT phase stays open to new messages even after the protocol moves on to
	// a new round. Late-arriving COMMITS can still (must) cause a local decision, *in that round*.
	if msg.Current.Step == COMMIT_PHASE && i.phase != DECIDE_PHASE {
		return i.tryCommit(msg.Current.Round)
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
			return false
		}
	} else if msg.Current.Step == DECIDE_PHASE {
		// DECIDE needs no justification
		return !msg.Current.Value.IsZero()
	}

	sigPayload := SignaturePayload(msg.Current.Instance, msg.Current.Round, msg.Current.Step, msg.Current.Value)
	if !i.host.Verify(pubKey, sigPayload, msg.Signature) {
		i.log("invalid signature on %v", msg)
		return false
	}

	return true
}

func (i *instance) VerifyJustification(justification Justification) error {
	signers := make([]PubKey, 0)
	setBits, err := justification.QuorumSignature.Signers.All(uint64(len(i.powerTable.Entries)))
	if err != nil {
		return fmt.Errorf("failed to get all set bits: %w", err)
	}

	justificationPower := NewStoragePower(0)
	var bitIndex int
	var bit uint64
	strongQuorum := false
	for bitIndex, bit = range setBits {
		if int(bit) >= len(i.powerTable.Entries) {
			return fmt.Errorf("invalid bit index %d", bit)
		}
		signers = append(signers, i.powerTable.Entries[bit].PubKey)
		justificationPower.Add(justificationPower, i.powerTable.Entries[bit].Power)
		if hasStrongQuorum(justificationPower, i.powerTable.Total) {
			strongQuorum = true
			break // no need to keep calculating
		}
	}

	if !strongQuorum {
		return fmt.Errorf("No evidence from a strong quorum: %v", justification.QuorumSignature.Signers)
	}

	// need to retrieve the remaining pubkeys just to verify the aggregate
	//TODO we could enforce here a tight strong quorum and no extra signatures
	// to prevent wasting time in verifying too many aggregated signatures
	// but should be careful with oligopolies (check out issue #49)
	// A deterministic but drand-sourced permutation could prevent oligopolies
	// if a random permutation affects performance we could do a weighted random permutation
	// To favour with more power.
	for bitIndex++; bitIndex < len(setBits); bitIndex++ {
		bit = setBits[bitIndex]
		if int(bit) >= len(i.powerTable.Entries) {
			return fmt.Errorf("invalid bit index %d", bit)
		}
		signers = append(signers, i.powerTable.Entries[bit].PubKey)
	}

	payload := SignaturePayload(justification.Payload.Instance, justification.Payload.Round, justification.Payload.Step, justification.Payload.Value)
	if !i.host.VerifyAggregate(payload, justification.QuorumSignature.Signature, signers) {
		return fmt.Errorf("Invalid justification signature: %v", justification)
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
		if msg.Justification.Payload.Step != COMMIT_PHASE {
			return fmt.Errorf("dropping DECIDE %v with evidence from wrong step %v", msg.Current.Round, msg.Justification.Payload.Step)
		}
		if msg.Current.Value.IsZero() || msg.Justification.Payload.Value.IsZero() {
			return fmt.Errorf("dropping DECIDE %v with evidence for a zero value: %v", msg.Current.Value, msg.Justification.Payload.Value)
		}
		if msg.Current.Value.Head().CID != msg.Justification.Payload.Value.Head().CID {
			return fmt.Errorf("dropping DECIDE %v with evidence for a different value: %v", msg.Current.Value, msg.Justification.Payload.Value)
		}
		return nil

	default:
		return fmt.Errorf("unknown message step: %v", msg.Current.Step)
	}

	if msg.Current.Instance != msg.Justification.Payload.Instance {
		return fmt.Errorf("message with instanceID %v has evidence from wrong instanceID: %v", msg.Current.Instance, msg.Justification.Payload.Instance)
	}

	return i.VerifyJustification(msg.Justification)

}

// Sends this node's QUALITY message and begins the QUALITY phase.
func (i *instance) beginQuality() error {
	if i.phase != INITIAL_PHASE {
		return fmt.Errorf("cannot transition from %s to %s", i.phase, QUALITY_PHASE)
	}
	// Broadcast input value and wait up to Δ to receive from others.
	i.phase = QUALITY_PHASE
	i.phaseTimeout = i.alarmAfterSynchrony(QUALITY_PHASE.String())
	i.broadcast(i.round, QUALITY_PHASE, i.input, nil, Justification{})
	return nil
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

func (i *instance) beginConverge() error {
	i.phase = CONVERGE_PHASE
	ticket := i.vrf.MakeTicket(i.beacon, i.instanceID, i.round, i.participantID)
	i.phaseTimeout = i.alarmAfterSynchrony(CONVERGE_PHASE.String())
	prevRoundState := i.roundState(i.round - 1)
	var justification Justification
	var ok bool
	if prevRoundState.committed.HasStrongQuorumAgreement(ZeroTipSetID()) {
		value := ECChain{}
		signers, err := prevRoundState.committed.getStrongQuorumSigners(value)
		if err != nil {
			return err
		}
		signatures, err := prevRoundState.committed.getSignatures(value, signers)
		if err != nil {
			return err
		}
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
		signers, err := prevRoundState.prepared.getStrongQuorumSigners(value)
		if err != nil {
			return err
		}
		signatures, err := prevRoundState.prepared.getSignatures(value, signers)
		if err != nil {
			return err
		}
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
		return fmt.Errorf("beginConverge called but no evidence found")
	}
	i.broadcast(i.round, CONVERGE_PHASE, i.proposal, ticket, justification)
	return nil
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
	i.phase = PREPARE_PHASE
	i.phaseTimeout = i.alarmAfterSynchrony(PREPARE_PHASE.String())
	i.broadcast(i.round, PREPARE_PHASE, i.value, nil, Justification{})
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
		if err := i.beginCommit(); err != nil {
			return err
		}
	}

	return nil
}

func (i *instance) beginCommit() error {
	i.phase = COMMIT_PHASE
	i.phaseTimeout = i.alarmAfterSynchrony(PREPARE_PHASE.String())
	signers := i.roundState(i.round).prepared.getSigners(i.value)
	signatures, err := i.roundState(i.round).prepared.getSignatures(i.value, signers)
	if err != nil {
		return err
	}
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
	justification := Justification{
		Payload:         justificationPayload,
		QuorumSignature: justificationSignature,
	}
	i.broadcast(i.round, COMMIT_PHASE, i.value, nil, justification)
	return nil
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
		if err := i.beginDecide(round); err != nil {
			return err
		}
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

		if err := i.beginNextRound(); err != nil {
			return err
		}
	}

	return nil
}

func (i *instance) beginDecide(round uint64) error {
	i.phase = DECIDE_PHASE
	roundState := i.roundState(round)
	signers, err := roundState.committed.getStrongQuorumSigners(i.value)
	if err != nil {
		return err
	}
	signatures, err := roundState.committed.getSignatures(i.value, signers)
	if err != nil {
		return err
	}
	aggSignature := make([]byte, 0)
	for _, sig := range signatures {
		aggSignature = i.host.Aggregate([][]byte{sig}, aggSignature)
	}
	justificationPayload := SignedMessage{
		Instance: i.instanceID,
		Round:    round,
		Step:     COMMIT_PHASE,
		Value:    i.value,
	}
	justificationSignature := QuorumSignature{
		Signers:   signers,
		Signature: aggSignature,
	}
	justification := Justification{
		Payload:         justificationPayload,
		QuorumSignature: justificationSignature,
	}
	i.broadcast(0, DECIDE_PHASE, i.value, nil, justification)
	return nil
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

func (i *instance) beginNextRound() error {
	i.round += 1
	i.log("moving to round %d with %s", i.round, i.proposal.String())
	if err := i.beginConverge(); err != nil {
		return err
	}
	return nil
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
	sigCopy := make([]byte, len(signature))
	copy(sigCopy, signature)
	if ok {
		// Don't double-count the same chain head for a single participant.
		if _, ok := fromSender.heads[head]; ok {
			return
		}
		fromSender.heads[head] = sigCopy
	} else {
		// Add sender's power to total the first time a value is received from them.
		q.sendersTotalPower.Add(q.sendersTotalPower, senderPower)
		fromSender = senderSent{
			heads: map[TipSetID][]byte{
				head: sigCopy,
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

	if !value.IsZero() && justification.Payload.Step == PREPARE_PHASE { //only committed roundStates need to store justifications
		q.justifiedMessages[value.Head().CID] = justification
	}
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
	for key := range chainSupport.signers {
		signers.Set(uint64(q.powerTable.Lookup[key]))
	}

	return signers
}

// getStrongQuorumSigners retrieves just a strong quorum of signers of the given ECChain.
// At the moment, this is the signers with the most power until reaching a strong quorum.
func (q *quorumState) getStrongQuorumSigners(value ECChain) (bitfield.BitField, error) {
	signers := q.getSigners(value)
	strongQuorumSigners := bitfield.New()
	justificationPower := NewStoragePower(0)
	setBits, err := signers.All(uint64(len(q.powerTable.Entries)))
	if err != nil {
		return bitfield.New(), err
	}
	for _, bit := range setBits {
		justificationPower.Add(justificationPower, q.powerTable.Entries[bit].Power)
		strongQuorumSigners.Set(bit)
		if hasStrongQuorum(justificationPower, q.powerTable.Total) {
			break // no need to keep calculating
		}
	}
	if !hasStrongQuorum(justificationPower, q.powerTable.Total) {
		// if we didn't find a strong quorum, return an empty bitfield
		return bitfield.New(), fmt.Errorf("no strong quorum found")
	}
	return strongQuorumSigners, nil
}

// getSignatures returns the corresponding signatures for a given bitset of signers
func (q *quorumState) getSignatures(value ECChain, signers bitfield.BitField) ([][]byte, error) {
	head := value.HeadCIDOrZero()
	signatures := make([][]byte, 0)
	if err := signers.ForEach(func(bit uint64) error {
		if int(bit) >= len(q.powerTable.Entries) {
			return fmt.Errorf("invalid signer index: %d", bit)
		}
		if signature, ok := q.received[q.powerTable.Entries[bit].ID].heads[head]; ok {
			if len(signature) == 0 {
				return fmt.Errorf("signature is 0")
			}
			signatures = append(signatures, signature)
		} else {
			return fmt.Errorf("QuorumSignature not found")
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return signatures, nil
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
