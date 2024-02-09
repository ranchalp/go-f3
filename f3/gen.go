// Code generated by github.com/whyrusleeping/cbor-gen. DO NOT EDIT.

package f3

import (
	"fmt"
	"io"
	"math"
	"sort"

	cid "github.com/ipfs/go-cid"
	cbg "github.com/whyrusleeping/cbor-gen"
	xerrors "golang.org/x/xerrors"
)

var _ = xerrors.Errorf
var _ = cid.Undef
var _ = math.E
var _ = sort.Sort

var lengthBufGMessage = []byte{133}

func (t *GMessage) MarshalCBOR(w io.Writer) error {
	return nil
}

func (t *GMessage) UnmarshalCBOR(r io.Reader) (err error) {
	*t = GMessage{}

	cr := cbg.NewCborReader(r)

	maj, extra, err := cr.ReadHeader()
	if err != nil {
		return err
	}
	defer func() {
		if err == io.EOF {
			err = io.ErrUnexpectedEOF
		}
	}()

	if maj != cbg.MajArray {
		return fmt.Errorf("cbor input should be of type array")
	}

	if extra != 5 {
		return fmt.Errorf("cbor input had wrong number of fields")
	}

	// t.Sender (f3.ActorID) (uint64)

	{

		maj, extra, err = cr.ReadHeader()
		if err != nil {
			return err
		}
		if maj != cbg.MajUnsignedInt {
			return fmt.Errorf("wrong type for uint64 field")
		}
		t.Sender = ActorID(extra)

	}
	// t.Current (f3.SignedMessage) (struct)

	{

		if err := t.Vote.UnmarshalCBOR(cr); err != nil {
			return xerrors.Errorf("unmarshaling t.Current: %w", err)
		}

	}
	// t.Ticket (f3.Ticket) (slice)

	maj, extra, err = cr.ReadHeader()
	if err != nil {
		return err
	}

	if extra > cbg.ByteArrayMaxLen {
		return fmt.Errorf("t.Ticket: byte array too large (%d)", extra)
	}
	if maj != cbg.MajByteString {
		return fmt.Errorf("expected byte array")
	}

	if extra > 0 {
		t.Ticket = make([]uint8, extra)
	}

	if _, err := io.ReadFull(cr, t.Ticket); err != nil {
		return err
	}

	// t.Signature ([]uint8) (slice)

	maj, extra, err = cr.ReadHeader()
	if err != nil {
		return err
	}

	if extra > cbg.ByteArrayMaxLen {
		return fmt.Errorf("t.Signature: byte array too large (%d)", extra)
	}
	if maj != cbg.MajByteString {
		return fmt.Errorf("expected byte array")
	}

	if extra > 0 {
		t.Signature = make([]uint8, extra)
	}

	if _, err := io.ReadFull(cr, t.Signature); err != nil {
		return err
	}

	// t.Justification (f3.Justification) (struct)

	{

		if err := t.Justification.UnmarshalCBOR(cr); err != nil {
			return xerrors.Errorf("unmarshaling t.Justification: %w", err)
		}

	}
	return nil
}

var lengthBufSignedMessage = []byte{132}

func (t *Payload) MarshalCBOR(w io.Writer) error {
	if t == nil {
		_, err := w.Write(cbg.CborNull)
		return err
	}

	cw := cbg.NewCborWriter(w)

	if _, err := cw.Write(lengthBufSignedMessage); err != nil {
		return err
	}

	// t.Instance (uint64) (uint64)

	if err := cw.WriteMajorTypeHeader(cbg.MajUnsignedInt, uint64(t.Instance)); err != nil {
		return err
	}

	// t.Round (uint64) (uint64)

	if err := cw.WriteMajorTypeHeader(cbg.MajUnsignedInt, uint64(t.Round)); err != nil {
		return err
	}

	// t.Step (f3.Phase) (uint8)
	if err := cw.WriteMajorTypeHeader(cbg.MajUnsignedInt, uint64(t.Step)); err != nil {
		return err
	}

	// t.Value (f3.ECChain) (slice)

	if err := cw.WriteMajorTypeHeader(cbg.MajArray, uint64(len(t.Value))); err != nil {
		return err
	}
	for _, v := range t.Value {
		if err := v.MarshalCBOR(cw); err != nil {
			return err
		}

	}
	return nil
}

func (t *Payload) UnmarshalCBOR(r io.Reader) (err error) {
	*t = Payload{}

	cr := cbg.NewCborReader(r)

	maj, extra, err := cr.ReadHeader()
	if err != nil {
		return err
	}
	defer func() {
		if err == io.EOF {
			err = io.ErrUnexpectedEOF
		}
	}()

	if maj != cbg.MajArray {
		return fmt.Errorf("cbor input should be of type array")
	}

	if extra != 4 {
		return fmt.Errorf("cbor input had wrong number of fields")
	}

	// t.Instance (uint64) (uint64)

	{

		maj, extra, err = cr.ReadHeader()
		if err != nil {
			return err
		}
		if maj != cbg.MajUnsignedInt {
			return fmt.Errorf("wrong type for uint64 field")
		}
		t.Instance = uint64(extra)

	}
	// t.Round (uint64) (uint64)

	{

		maj, extra, err = cr.ReadHeader()
		if err != nil {
			return err
		}
		if maj != cbg.MajUnsignedInt {
			return fmt.Errorf("wrong type for uint64 field")
		}
		t.Round = uint64(extra)

	}
	// t.Step (f3.Phase) (uint8)

	maj, extra, err = cr.ReadHeader()
	if err != nil {
		return err
	}
	if maj != cbg.MajUnsignedInt {
		return fmt.Errorf("wrong type for uint8 field")
	}
	if extra > math.MaxUint8 {
		return fmt.Errorf("integer in input was too large for uint8 field")
	}
	t.Step = Phase(extra)
	// t.Value (f3.ECChain) (slice)

	maj, extra, err = cr.ReadHeader()
	if err != nil {
		return err
	}

	if extra > cbg.MaxLength {
		return fmt.Errorf("t.Value: array too large (%d)", extra)
	}

	if maj != cbg.MajArray {
		return fmt.Errorf("expected cbor array")
	}

	if extra > 0 {
		t.Value = make([]TipSet, extra)
	}

	for i := 0; i < int(extra); i++ {
		{
			var maj byte
			var extra uint64
			var err error
			_ = maj
			_ = extra
			_ = err

			{

				if err := t.Value[i].UnmarshalCBOR(cr); err != nil {
					return xerrors.Errorf("unmarshaling t.Value[i]: %w", err)
				}

			}

		}
	}
	return nil
}

var lengthBufJustification = []byte{130}

func (t *Justification) MarshalCBOR(w io.Writer) error {
	if t == nil {
		_, err := w.Write(cbg.CborNull)
		return err
	}

	cw := cbg.NewCborWriter(w)

	if _, err := cw.Write(lengthBufJustification); err != nil {
		return err
	}

	// t.Payload (f3.SignedMessage) (struct)
	if err := t.Vote.MarshalCBOR(cw); err != nil {
		return err
	}

	// t.QuorumSignature (f3.QuorumSignature) (struct)
	return nil
}

func (t *Justification) UnmarshalCBOR(r io.Reader) (err error) {
	*t = Justification{}

	cr := cbg.NewCborReader(r)

	maj, extra, err := cr.ReadHeader()
	if err != nil {
		return err
	}
	defer func() {
		if err == io.EOF {
			err = io.ErrUnexpectedEOF
		}
	}()

	if maj != cbg.MajArray {
		return fmt.Errorf("cbor input should be of type array")
	}

	if extra != 2 {
		return fmt.Errorf("cbor input had wrong number of fields")
	}

	// t.Payload (f3.SignedMessage) (struct)

	{

		if err := t.Vote.UnmarshalCBOR(cr); err != nil {
			return xerrors.Errorf("unmarshaling t.Payload: %w", err)
		}

	}
	// t.QuorumSignature (f3.QuorumSignature) (struct)

	{

	}
	return nil
}

var lengthBufQuorumSignature = []byte{130}

var lengthBufTipSet = []byte{131}

func (t *TipSet) MarshalCBOR(w io.Writer) error {
	if t == nil {
		_, err := w.Write(cbg.CborNull)
		return err
	}

	cw := cbg.NewCborWriter(w)

	if _, err := cw.Write(lengthBufTipSet); err != nil {
		return err
	}

	// t.Epoch (int64) (int64)
	if t.Epoch >= 0 {
		if err := cw.WriteMajorTypeHeader(cbg.MajUnsignedInt, uint64(t.Epoch)); err != nil {
			return err
		}
	} else {
		if err := cw.WriteMajorTypeHeader(cbg.MajNegativeInt, uint64(-t.Epoch-1)); err != nil {
			return err
		}
	}

	// t.CID (f3.TipSetID) (struct)
	if err := t.CID.MarshalCBOR(cw); err != nil {
		return err
	}

	// t.Weight (uint64) (uint64)

	if err := cw.WriteMajorTypeHeader(cbg.MajUnsignedInt, uint64(t.Weight)); err != nil {
		return err
	}

	return nil
}

func (t *TipSet) UnmarshalCBOR(r io.Reader) (err error) {
	*t = TipSet{}

	cr := cbg.NewCborReader(r)

	maj, extra, err := cr.ReadHeader()
	if err != nil {
		return err
	}
	defer func() {
		if err == io.EOF {
			err = io.ErrUnexpectedEOF
		}
	}()

	if maj != cbg.MajArray {
		return fmt.Errorf("cbor input should be of type array")
	}

	if extra != 3 {
		return fmt.Errorf("cbor input had wrong number of fields")
	}

	// t.Epoch (int64) (int64)
	{
		maj, extra, err := cr.ReadHeader()
		var extraI int64
		if err != nil {
			return err
		}
		switch maj {
		case cbg.MajUnsignedInt:
			extraI = int64(extra)
			if extraI < 0 {
				return fmt.Errorf("int64 positive overflow")
			}
		case cbg.MajNegativeInt:
			extraI = int64(extra)
			if extraI < 0 {
				return fmt.Errorf("int64 negative overflow")
			}
			extraI = -1 - extraI
		default:
			return fmt.Errorf("wrong type for int64 field: %d", maj)
		}

		t.Epoch = int64(extraI)
	}
	// t.CID (f3.TipSetID) (struct)

	{

		if err := t.CID.UnmarshalCBOR(cr); err != nil {
			return xerrors.Errorf("unmarshaling t.CID: %w", err)
		}

	}
	// t.Weight (uint64) (uint64)

	{

		maj, extra, err = cr.ReadHeader()
		if err != nil {
			return err
		}
		if maj != cbg.MajUnsignedInt {
			return fmt.Errorf("wrong type for uint64 field")
		}
		t.Weight = uint64(extra)

	}
	return nil
}
