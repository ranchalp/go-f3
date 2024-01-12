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

const QUALITY = "QUALITY"
const CONVERGE = "CONVERGE"
const PREPARE = "PREPARE"
const COMMIT = "COMMIT"
const DECIDE = "DECIDE"
const TERMINATED = "TERMINATED"

const DOMAIN_SEPARATION_TAG = "GPBFT"

type GMessage struct {
	// ID of the sender/signer of this message (a miner actor ID).
	Sender ActorID
	// GossiPBFT instance (epoch) number.
	Instance uint32
	// GossiPBFT round number.
	Round uint32
	// GossiPBFT step name.
	Step string
	// Chain of tipsets proposed/voted for finalisation.
	// Always non-empty; the first entry is the base tipset finalised in the previous instance.
	Value ECChain
	// VRF ticket for CONVERGE messages (otherwise empty byte array).
	Ticket Ticket
	// Signature by the sender's public key over Instance || Round || Step || Value.
	Signature []byte

	Evidence AggEvidence
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

func (m GMessage) String() string {
	// FIXME This needs value receiver to work, for reasons I cannot figure out.
	return fmt.Sprintf("%s{%d}(%d %s)", m.Step, m.Instance, m.Round, &m.Value)
}

// Computes the payload for a GMessage signature.
func SignaturePayload(instance uint32, round uint32, step string, value ECChain) []byte {
	var buf bytes.Buffer
	buf.WriteString(DOMAIN_SEPARATION_TAG)
	_ = binary.Write(&buf, binary.BigEndian, instance)
	_ = binary.Write(&buf, binary.BigEndian, round)
	buf.WriteString(step)
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
	instanceID    uint32
	// The EC chain input to this instance.
	input ECChain
	// The power table for the base chain, used for power in this instance.
	powerTable PowerTable
	// The beacon value from the base chain, used for tickets in this instance.
	beacon []byte
	// Current round number.
	round uint32
	// Current phase in the round.
	phase string
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
	rounds map[uint32]*roundState
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
	instanceID uint32,
	input ECChain,
	powerTable PowerTable,
	beacon []byte) *instance {
	if input.IsZero() {
		panic("input is empty")
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
		phase:         "",
		proposal:      input,
		value:         ECChain{},
		quality:       newQuorumState(powerTable),
		rounds: map[uint32]*roundState{
			0: newRoundState(powerTable),
		},
		acceptable: input,
		decision:   newQuorumState(powerTable),
	}
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

func (i *instance) Start() {
	i.beginQuality()
	i.drainInbox()
}

// Receives a new acceptable chain and updates its current acceptable chain.
func (i *instance) receiveAcceptable(chain ECChain) {
	i.acceptable = chain
}

func (i *instance) Receive(msg *GMessage) {
	if i.terminated() {
		panic("received message after decision")
	}
	if len(i.inbox) > 0 {
		panic("received message while already processing inbox")
	}

	// Enqueue the message for synchronous processing.
	i.enqueueInbox(msg)
	i.drainInbox()
}

func (i *instance) ReceiveAlarm(_ string) {
	i.tryCompletePhase()

	// A phase may have been successfully completed.
	// Re-process any queued messages for the next phase.
	i.drainInbox()
}

func (i *instance) Describe() string {
	return fmt.Sprintf("P%d{%d}, round %d, phase %s", i.participantID, i.instanceID, i.round, i.phase)
}

func (i *instance) enqueueInbox(msg *GMessage) {
	i.inbox = append(i.inbox, msg)
}

func (i *instance) drainInbox() {
	for len(i.inbox) > 0 {
		// Process one message.
		// Note the message being processed is left in the inbox until after processing,
		// as a signal that this loop is currently draining the inbox.
		i.receiveOne(i.inbox[0])
		i.inbox = i.inbox[1:]
	}
}

// Processes a single message.
func (i *instance) receiveOne(msg *GMessage) {
	if i.phase == TERMINATED {
		return // No-op
	}

	// Drop any messages that can never be valid.
	if !i.isValid(msg) {
		i.log("dropping invalid %s", msg)
		return
	}

	if !i.isJustified(msg) {
		// No implicit justification:
		// if message not justified explicitly, then it will not be justified
		i.log("dropping unjustified %s", msg)
		return
	}

	round := i.roundState(msg.Round)
	switch msg.Step {
	case QUALITY:
		// Receive each prefix of the proposal independently.
		for j := range msg.Value.Suffix() {
			prefix := msg.Value.Prefix(j + 1)
			i.quality.Receive(msg.Sender, prefix, msg.Signature)
		}
	case CONVERGE:
		round.converged.Receive(msg.Value, msg.Ticket)
	case PREPARE:
		round.prepared.Receive(msg.Sender, msg.Value, msg.Signature)
	case COMMIT:
		round.committed.Receive(msg.Sender, msg.Value, msg.Signature)
	case DECIDE:
		i.decision.Receive(msg.Sender, msg.Value, msg.Signature)
	default:
		i.log("unexpected message %v", msg)
	}

	// Try to complete the current phase.
	// Every COMMIT phase stays open to new messages even after the protocol moves on to
	// a new round. Late-arriving COMMITS can still (must) cause a local decision, *in that round*.
	if msg.Step == COMMIT && i.phase != DECIDE {
		i.tryCommit(msg.Round)
	} else {
		i.tryCompletePhase()
	}
}

// Attempts to complete the current phase and round.
func (i *instance) tryCompletePhase() {
	i.log("try step %s", i.phase)
	switch i.phase {
	case QUALITY:
		i.tryQuality()
	case CONVERGE:
		i.tryConverge()
	case PREPARE:
		i.tryPrepare()
	case COMMIT:
		i.tryCommit(i.round)
	case DECIDE:
		i.tryDecide()
	case TERMINATED:
		return // No-op
	default:
		panic(fmt.Sprintf("unexpected phase %s", i.phase))
	}
}

// Checks whether a message is valid.
// An invalid message can never become valid, so may be dropped.
func (i *instance) isValid(msg *GMessage) bool {
	if !(msg.Value.IsZero() || msg.Value.HasBase(i.input.Base())) {
		i.log("unexpected base %s", &msg.Value)
		return false
	}
	if msg.Step == CONVERGE {
		if !i.vrf.VerifyTicket(i.beacon, i.instanceID, msg.Round, msg.Sender, msg.Ticket) {
			return false
		}
	}
	return true
}

// Checks whether a message is justified by prior messages.
// An unjustified message may later be justified by subsequent messages.
func (i *instance) isJustified(msg *GMessage) bool {
	if msg.Step == QUALITY {
		// QUALITY needs no justification by prior messages.
		return msg.Round == 0 && !msg.Value.IsZero()
	} else if msg.Step == CONVERGE {
		// CONVERGE is justified by a previous round strong quorum of PREPARE for the same value,
		// or strong quorum of COMMIT for bottom.
		// Bottom is not allowed as a value.
		if msg.Round == 0 || msg.Value.IsZero() {
			return false
		}

		prevRound := msg.Round - 1
		if msg.Evidence.Round != prevRound {
			i.log("dropping CONVERGE %s with evidence from wrong round %d", msg, msg.Evidence.Round)
			return false
		}

		if i.instanceID != msg.Evidence.Instance {
			i.log("dropping CONVERGE %s with evidence from wrong instanceID %v", msg, msg.Evidence.Instance)
			return false
		}

		if msg.Evidence.Step == PREPARE {
			if msg.Value.Head().CID != msg.Evidence.Value.Head().CID {
				i.log("dropping CONVERGE %s with PREPARE evidence for distinct value's head CID %v", msg, msg.Evidence.Value.Head().CID)
				return false
			}
		} else if msg.Evidence.Step == COMMIT {
			if msg.Evidence.Value.HeadCIDOrZero() != ZeroTipSetID() {
				i.log("dropping CONVERGE %s with COMMIT evidence for non-zero value %v", msg, msg.Evidence.Value)
				return false
			}
		} else {
			i.log("dropping CONVERGE %s with evidence from wrong step %s", msg, msg.Evidence.Step)
			return false
		}

		payload := SignaturePayload(i.instanceID, prevRound, msg.Evidence.Step, msg.Evidence.Value)
		if !i.host.VerifyAggregate(payload, msg.Evidence.Signature, &msg.Evidence.Signers, i.powerTable.Lookup) {
			i.log("dropping CONVERGE %s with invalid evidence signature", msg)
			return false
		}

		return true
	} else if msg.Step == PREPARE {
		// PREPARE needs no justification by prior messages.
		return true // i.quality.AllowsValue(msg.Value)
	} else if msg.Step == COMMIT {
		// COMMIT is justified by strong quorum of PREPARE from the same round with the same value.
		// COMMIT for bottom is always justified.
		return true
	} else if msg.Step == DECIDE {
		// DECIDE needs no justification
		return !msg.Value.IsZero()
	}
	return false
}

// Sends this node's QUALITY message and begins the QUALITY phase.
func (i *instance) beginQuality() {
	// Broadcast input value and wait up to Δ to receive from others.
	i.phase = QUALITY
	i.phaseTimeout = i.alarmAfterSynchrony(QUALITY)
	i.broadcast(i.round, QUALITY, i.input, nil, AggEvidence{})
}

// Attempts to end the QUALITY phase and begin PREPARE based on current state.
// No-op if the current phase is not QUALITY.
func (i *instance) tryQuality() {
	if i.phase != QUALITY {
		panic(fmt.Sprintf("unexpected phase %s", i.phase))
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
}

func (i *instance) beginConverge() {
	i.phase = CONVERGE
	ticket := i.vrf.MakeTicket(i.beacon, i.instanceID, i.round, i.participantID)
	i.phaseTimeout = i.alarmAfterSynchrony(CONVERGE)
	prevRoundState := i.roundState(i.round - 1)
	if prevRoundState.committed.HasStrongQuorumAgreement(ZeroTipSetID()) {
		getEvidence(ECChain{}, prevRoundState.committed)
	}
	i.broadcast(i.round, CONVERGE, i.proposal, ticket, AggEvidence{})
}

// FIXME As instructed I have attempted to keep quorumState as oblivious of singature aggregation as possible, but I think
// there are clear benefits to coupling (namely more efficient storage and processing of signatures by quorumState)
// this would be showcased in this auxiliary function especially, getEvidence.
func getEvidence(value ECChain, q *quorumState) AggEvidence {
	//TODO Continue, but discuss first
}

// Attempts to end the CONVERGE phase and begin PREPARE based on current state.
// No-op if the current phase is not CONVERGE.
func (i *instance) tryConverge() {
	if i.phase != CONVERGE {
		panic(fmt.Sprintf("unexpected phase %s", i.phase))
	}
	timeoutExpired := i.host.Time() >= i.phaseTimeout
	if !timeoutExpired {
		return
	}

	i.value = i.roundState(i.round).converged.findMinTicketProposal()
	if i.value.IsZero() {
		panic("no values at CONVERGE")
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
}

// Sends this node's PREPARE message and begins the PREPARE phase.
func (i *instance) beginPrepare() {
	// Broadcast preparation of value and wait for everyone to respond.
	i.phase = PREPARE
	i.phaseTimeout = i.alarmAfterSynchrony(PREPARE)
	i.broadcast(i.round, PREPARE, i.value, nil, AggEvidence{})
}

// Attempts to end the PREPARE phase and begin COMMIT based on current state.
// No-op if the current phase is not PREPARE.
func (i *instance) tryPrepare() {
	if i.phase != PREPARE {
		panic(fmt.Sprintf("unexpected phase %s", i.phase))
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
}

func (i *instance) beginCommit() {
	i.phase = COMMIT
	i.phaseTimeout = i.alarmAfterSynchrony(PREPARE)
	i.broadcast(i.round, COMMIT, i.value, nil, AggEvidence{})
}

func (i *instance) tryCommit(round uint32) {
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
	} else if i.round == round && i.phase == COMMIT && timeoutExpired && committed.ReceivedFromStrongQuorum() {
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
}

func (i *instance) beginDecide() {
	i.phase = DECIDE
	i.broadcast(0, DECIDE, i.value, nil, AggEvidence{})
}

func (i *instance) tryDecide() {
	foundQuorum := i.decision.ListStrongQuorumAgreedValues()
	if len(foundQuorum) > 0 {
		i.terminate(foundQuorum[0], i.round)
	}
}

func (i *instance) roundState(r uint32) *roundState {
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

func (i *instance) terminate(value ECChain, round uint32) {
	i.log("✅ terminated %s in round %d", &i.value, round)
	i.phase = TERMINATED
	// Round is a parameter since a late COMMIT message can result in a decision for a round prior to the current one.
	i.round = round
	i.value = value
}

func (i *instance) terminated() bool {
	return i.phase == TERMINATED
}

func (i *instance) broadcast(round uint32, step string, value ECChain, ticket Ticket, evidence AggEvidence) *GMessage {
	payload := SignaturePayload(i.instanceID, round, step, value)
	signature := i.host.Sign(i.participantID, payload)
	gmsg := &GMessage{i.participantID, i.instanceID, round, step, value, ticket, signature, evidence}
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
	chainPower map[TipSetID]chainPower
	// Total power of all distinct senders from which some chain has been received so far.
	sendersTotalPower uint
	// Table of senders' power.
	powerTable PowerTable
}

// The set of chain heads from one sender and associated signature, and that sender's power.
type senderSent struct {
	heads map[TipSetID][]byte
	power uint
}

// A chain value and the total power supporting it.
type chainPower struct {
	chain           ECChain
	power           uint
	hasStrongQuorum bool
	hasWeakQuorum   bool
}

// Creates a new, empty quorum state.
func newQuorumState(powerTable PowerTable) *quorumState {
	return &quorumState{
		received:          map[ActorID]senderSent{},
		chainPower:        map[TipSetID]chainPower{},
		sendersTotalPower: 0,
		powerTable:        powerTable,
	}
}

// Receives a new chain from a sender.
func (q *quorumState) Receive(sender ActorID, value ECChain, signature []byte) {
	head := value.HeadCIDOrZero()
	fromSender, ok := q.received[sender]
	if ok {
		// Don't double-count the same chain head for a single participant.
		if _, ok := fromSender.heads[head]; ok {
			return
		}
		fromSender.heads[head] = signature
	} else {
		// Add sender's power to total the first time a value is received from them.
		senderPower := q.powerTable.Entries[sender].Power
		q.sendersTotalPower += senderPower
		fromSender = senderSent{
			heads: map[TipSetID][]byte{
				head: signature,
			},
			power: senderPower,
		}
	}
	q.received[sender] = fromSender

	candidate := chainPower{
		chain:           value,
		power:           q.powerTable.Entries[sender].Power,
		hasStrongQuorum: false,
		hasWeakQuorum:   false,
	}
	if found, ok := q.chainPower[head]; ok {
		candidate.power += found.power
	}

	strongThreshold := q.powerTable.Total * 2 / 3
	if candidate.power > strongThreshold {
		candidate.hasStrongQuorum = true
	}

	weakThreshold := q.powerTable.Total * 1 / 3
	if candidate.power > weakThreshold {
		candidate.hasWeakQuorum = true
	}

	q.chainPower[head] = candidate
}

// Checks whether a value has been received before.
func (q *quorumState) HasReceived(value ECChain) bool {
	_, ok := q.chainPower[value.HeadCIDOrZero()]
	return ok
}

// Lists all values that have been received from any sender.
// The order of returned values is not defined.
func (q *quorumState) ListAllValues() []ECChain {
	var chains []ECChain
	for _, cp := range q.chainPower {
		chains = append(chains, cp.chain)
	}
	return chains
}

// Checks whether at most one distinct value has been received.
func (q *quorumState) HasAgreement() bool {
	return len(q.chainPower) <= 1
}

// Checks whether at least one message has been received from a strong quorum of senders.
func (q *quorumState) ReceivedFromStrongQuorum() bool {
	return q.sendersTotalPower > q.powerTable.Total*2/3
}

// Checks whether a chain (head) has reached a strong quorum.
func (q *quorumState) HasStrongQuorumAgreement(cid TipSetID) bool {
	cp, ok := q.chainPower[cid]
	return ok && cp.hasStrongQuorum
}

// Checks whether a chain (head) has reached weak quorum.
func (q *quorumState) HasWeakQuorumAgreement(cid TipSetID) bool {
	cp, ok := q.chainPower[cid]
	return ok && cp.hasWeakQuorum
}

// Returns a list of the chains which have reached an agreeing strong quorum.
// The order of returned values is not defined.
func (q *quorumState) ListStrongQuorumAgreedValues() []ECChain {
	var withQuorum []ECChain
	for cid, cp := range q.chainPower {
		if cp.hasStrongQuorum {
			withQuorum = append(withQuorum, q.chainPower[cid].chain)
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
func (c *convergeState) Receive(value ECChain, ticket Ticket) {
	if value.IsZero() {
		panic("bottom cannot be justified for CONVERGE")
	}
	key := value.Head().CID
	c.values[key] = value
	c.tickets[key] = append(c.tickets[key], ticket)
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
