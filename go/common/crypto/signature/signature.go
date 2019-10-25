// Package signature provides wrapper types around public key signatures.
package signature

import (
	"bytes"
	"crypto/rand"
	"encoding"
	"encoding/base64"
	"encoding/hex"
	encPem "encoding/pem"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"sync"

	"github.com/oasislabs/ed25519"
	"github.com/oasislabs/oasis-core/go/common/cbor"
	"github.com/oasislabs/oasis-core/go/common/pem"
	"github.com/oasislabs/oasis-core/go/grpc/common"
)

const (
	// PublicKeySize is the size of a public key in bytes.
	PublicKeySize = ed25519.PublicKeySize

	// SignatureSize is the size of a signature in bytes.
	SignatureSize = ed25519.SignatureSize

	pubPEMType = "ED25519 PUBLIC KEY"
	sigPEMType = "ED25519 SIGNATURE"
	filePerm   = 0600
)

var (
	// ErrMalformedPublicKey is the error returned when a public key is
	// malformed.
	ErrMalformedPublicKey = errors.New("signature: malformed public key")

	// ErrMalformedSignature is the error returned when a signature is
	// malformed.
	ErrMalformedSignature = errors.New("signature: malformed signature")

	// ErrPublicKeyMismatch is the error returned when a signature was
	// not produced by the expected public key.
	ErrPublicKeyMismatch = errors.New("signature: public key mismatch")

	// ErrNilProtobuf is the error returned when a protobuf is nil.
	ErrNilProtobuf = errors.New("signature: protobuf is nil")

	// ErrVerifyFailed is the error return when a signature verification
	// fails when opening a signed blob.
	ErrVerifyFailed = errors.New("signed: signature verification failed")

	errKeyMismatch = errors.New("signature: public key PEM is not for private key")

	_ cbor.Marshaler             = PublicKey{}
	_ cbor.Unmarshaler           = (*PublicKey)(nil)
	_ cbor.Marshaler             = (*Signed)(nil)
	_ cbor.Unmarshaler           = (*Signed)(nil)
	_ encoding.BinaryMarshaler   = PublicKey{}
	_ encoding.BinaryUnmarshaler = (*PublicKey)(nil)
	_ encoding.BinaryMarshaler   = RawSignature{}
	_ encoding.BinaryUnmarshaler = (*RawSignature)(nil)

	testPublicKeys        sync.Map
	blacklistedPublicKeys sync.Map

	defaultOptions = &ed25519.Options{}
)

// MapKey is a PublicKey as a fixed sized byte array for use as a map key.
type MapKey [PublicKeySize]byte

// String returns a string representation of the MapKey.
func (k MapKey) String() string {
	return hex.EncodeToString(k[:])
}

// MarshalBinary encodes a public key into binary form.
func (k MapKey) MarshalBinary() (data []byte, err error) {
	data = append([]byte{}, k[:]...)
	return
}

// UnmarshalBinary decodes a binary marshaled public key.
func (k *MapKey) UnmarshalBinary(data []byte) error {
	if len(data) != PublicKeySize {
		return ErrMalformedPublicKey
	}

	copy((*k)[:], data)

	return nil
}

// MarshalText encodes a public key into text form.
func (k MapKey) MarshalText() (data []byte, err error) {
	return []byte(base64.StdEncoding.EncodeToString(k[:])), nil
}

// UnmarshalText decodes a text marshaled public key.
func (k *MapKey) UnmarshalText(text []byte) error {
	b, err := base64.StdEncoding.DecodeString(string(text))
	if err != nil {
		return err
	}

	return k.UnmarshalBinary(b)
}

// PublicKey is a public key used for signing.
type PublicKey ed25519.PublicKey

// Verify returns true iff the signature is valid for the public key
// over the context and message.
func (k PublicKey) Verify(context, message, sig []byte) bool {
	if len(k) != PublicKeySize {
		return false
	}
	if len(sig) != SignatureSize {
		return false
	}
	if k.isBlacklisted() {
		return false
	}

	data, err := PrepareSignerMessage(context, message)
	if err != nil {
		return false
	}

	return ed25519.Verify(ed25519.PublicKey(k), data, sig)
}

// MarshalBinary encodes a public key into binary form.
func (k PublicKey) MarshalBinary() (data []byte, err error) {
	data = append([]byte{}, k[:]...)
	return
}

// UnmarshalBinary decodes a binary marshaled public key.
func (k *PublicKey) UnmarshalBinary(data []byte) error {
	// HACK: go-codec apparently was skipping calls to UnmarshalBinary
	// or something, while the new library will always call it.
	//
	// We have approximately 3 million different places where we use
	// the default value for public keys, so special case it.
	if len(data) == 0 {
		*k = nil
		return nil
	}

	if len(data) != PublicKeySize {
		return ErrMalformedPublicKey
	}

	if len(*k) != PublicKeySize {
		keybuf := make([]byte, PublicKeySize)
		*k = keybuf
	}
	copy((*k)[:], data)

	return nil
}

// MarshalCBOR serializes the type into a CBOR byte vector.
func (k PublicKey) MarshalCBOR() []byte {
	return cbor.Marshal(k)
}

// UnmarshalCBOR deserializes a CBOR byte vector into given type.
func (k *PublicKey) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, k)
}

// UnmarshalPEM decodes a PEM marshaled PublicKey.
func (k *PublicKey) UnmarshalPEM(data []byte) error {
	b, err := pem.Unmarshal(pubPEMType, data)
	if err != nil {
		return err
	}

	return k.UnmarshalBinary(b)
}

// MarshalPEM encodes a PublicKey into PEM form.
func (k PublicKey) MarshalPEM() (data []byte, err error) {
	return pem.Marshal(pubPEMType, k[:])
}

// UnmarshalHex deserializes a hexadecimal text string into the given type.
func (k *PublicKey) UnmarshalHex(text string) error {
	b, err := hex.DecodeString(text)
	if err != nil {
		return err
	}

	return k.UnmarshalBinary(b)
}

// Equal compares vs another public key for equality.
func (k PublicKey) Equal(cmp PublicKey) bool {
	return bytes.Equal(k, cmp)
}

// String returns a string representation of the public key.
func (k PublicKey) String() string {
	hexKey := hex.EncodeToString(k)

	if len(k) != PublicKeySize {
		return "[malformed]: " + hexKey
	}

	return hexKey
}

// ToMapKey returns a fixed-sized representation of the public key.
func (k PublicKey) ToMapKey() MapKey {
	if len(k) != PublicKeySize {
		panic("signature: public key invalid size for ID")
	}

	var mk MapKey
	copy(mk[:], k)

	return mk
}

// FromMapKey converts a MapKey back to a public key.
func (k *PublicKey) FromMapKey(mk MapKey) {
	if err := k.UnmarshalBinary(mk[:]); err != nil {
		panic("signature: failed to convert MapKey: " + err.Error())
	}
}

// LoadPEM loads a public key from a PEM file on disk.  Iff the public key
// is missing and a Signer is provided, the Signer's corresponding
// public key will be written and loaded.
func (k *PublicKey) LoadPEM(fn string, signer Signer) error {
	f, err := os.Open(fn) // nolint: gosec
	if err != nil {
		if os.IsNotExist(err) && signer != nil {
			pubKey := signer.Public()

			var buf []byte
			if buf, err = pubKey.MarshalPEM(); err != nil {
				return err
			}

			copy((*k)[:], pubKey[:])

			return ioutil.WriteFile(fn, buf, filePerm)
		}
		return err
	}
	defer f.Close() // nolint: errcheck

	buf, err := ioutil.ReadAll(f)
	if err != nil {
		return err
	}

	if err = k.UnmarshalPEM(buf); err != nil {
		return err
	}

	if signer != nil && !k.Equal(signer.Public()) {
		return errKeyMismatch
	}

	return nil
}

func (k *PublicKey) isBlacklisted() bool {
	_, isBlacklisted := blacklistedPublicKeys.Load(k.ToMapKey())
	return isBlacklisted
}

// RawSignature is a raw signature.
type RawSignature [SignatureSize]byte

// MarshalBinary encodes a signature into binary form.
func (r RawSignature) MarshalBinary() (data []byte, err error) {
	data = append([]byte{}, r[:]...)
	return
}

// UnmarshalBinary decodes a binary marshaled signature.
func (r *RawSignature) UnmarshalBinary(data []byte) error {
	if len(data) != SignatureSize {
		return ErrMalformedSignature
	}

	copy(r[:], data)

	return nil
}

// MarshalText encodes a signature into text form.
func (r RawSignature) MarshalText() (data []byte, err error) {
	return []byte(base64.StdEncoding.EncodeToString(r[:])), nil
}

// UnmarshalText decodes a text marshaled signature.
func (r *RawSignature) UnmarshalText(text []byte) error {
	b, err := base64.StdEncoding.DecodeString(string(text))
	if err != nil {
		return err
	}

	return r.UnmarshalBinary(b)
}

// MarshalPEM encodes a raw signature into PEM format.
func (r RawSignature) MarshalPEM() (data []byte, err error) {
	return pem.Marshal(sigPEMType, r[:])
}

// UnmarshalPEM decodes a PEM marshaled raw signature.
func (r *RawSignature) UnmarshalPEM(data []byte) error {
	sig, err := pem.Unmarshal(sigPEMType, data)
	if err != nil {
		return err
	}
	copy(r[:], sig)

	return nil
}

// Signature is a signature, bundled with the signing public key.
type Signature struct {
	// PublicKey is the public key that produced the signature.
	PublicKey PublicKey `json:"public_key"`

	// Signature is the actual raw signature.
	Signature RawSignature `json:"signature"`
}

// Sign generates a signature with the private key over the context and
// message.
func Sign(signer Signer, context, message []byte) (*Signature, error) {
	signature, err := signer.ContextSign(context, message)
	if err != nil {
		return nil, err
	}

	var rawSignature RawSignature
	if err = rawSignature.UnmarshalBinary(signature); err != nil {
		return nil, err
	}

	return &Signature{PublicKey: signer.Public(), Signature: rawSignature}, nil
}

// Verify returns true iff the signature is valid over the given
// context and message.
func (s *Signature) Verify(context, message []byte) bool {
	return s.PublicKey.Verify(context, message, s.Signature[:])
}

// SanityCheck checks if the signature appears to be well formed.
func (s *Signature) SanityCheck(expectedPubKey PublicKey) error {
	if len(s.PublicKey) != PublicKeySize {
		return ErrMalformedPublicKey
	}
	if !s.PublicKey.Equal(expectedPubKey) {
		return ErrPublicKeyMismatch
	}
	if len(s.Signature) != SignatureSize {
		return ErrMalformedSignature
	}
	return nil
}

// FromProto deserializes a protobuf into a Signature.
func (s *Signature) FromProto(pb *common.Signature) error {
	if pb == nil {
		return ErrNilProtobuf
	}

	if err := s.PublicKey.UnmarshalBinary(pb.GetPubkey()); err != nil {
		return err
	}
	if err := s.Signature.UnmarshalBinary(pb.GetSignature()); err != nil {
		return err
	}

	return nil
}

// ToProto serializes a protobuf version of the Signature.
func (s *Signature) ToProto() *common.Signature {
	pb := new(common.Signature)

	pb.Pubkey, _ = s.PublicKey.MarshalBinary()
	pb.Signature, _ = s.Signature.MarshalBinary()

	return pb
}

// MarshalPEM encodes a signature into PEM format.
func (s Signature) MarshalPEM() (data []byte, err error) {
	pk, err := s.PublicKey.MarshalPEM()
	if err != nil {
		return []byte{}, err
	}

	sig, err := s.Signature.MarshalPEM()
	if err != nil {
		return []byte{}, err
	}

	return bytes.Join([][]byte{pk, sig}, []byte{}), nil
}

// UnmarshalPem decodes a PEM marshaled signature.
func (s *Signature) UnmarshalPEM(data []byte) error {
	// Marshalled PEM file contains public key block first...
	blk, rest := encPem.Decode(data)
	if blk == nil {
		return fmt.Errorf("signature: error while decoding PEM block %s", pubPEMType)
	}

	if blk.Type != pubPEMType {
		return fmt.Errorf("signature: expected different PEM block (expected: %s got: %s)", pubPEMType, blk.Type)
	}
	if err := s.PublicKey.UnmarshalBinary(blk.Bytes); err != nil {
		return err
	}

	// ...and then raw signature.
	blk, _ = encPem.Decode(rest)
	if blk == nil {
		return fmt.Errorf("signature: error while decoding PEM block %s", sigPEMType)
	}

	if blk.Type != sigPEMType {
		return fmt.Errorf("signature: expected different PEM block (expected: %s got: %s)", sigPEMType, blk.Type)
	}
	if err := s.Signature.UnmarshalBinary(blk.Bytes); err != nil {
		return err
	}

	return nil
}

// Signed is a signed blob.
type Signed struct {
	// Blob is the signed blob.
	Blob []byte `json:"untrusted_raw_value"`

	// Signature is the signature over blob.
	Signature Signature `json:"signature"`
}

// SignSigned generates a Signed with the Signer over the context and
// CBOR-serialized message.
func SignSigned(signer Signer, context []byte, src cbor.Marshaler) (*Signed, error) {
	data := src.MarshalCBOR()
	signature, err := Sign(signer, context, data)
	if err != nil {
		return nil, err
	}

	return &Signed{Blob: data, Signature: *signature}, nil
}

// Open first verifies the blob signature and then unmarshals the blob.
func (s *Signed) Open(context []byte, dst cbor.Unmarshaler) error {
	// Verify signature first.
	if !s.Signature.Verify(context, s.Blob) {
		return ErrVerifyFailed
	}

	return dst.UnmarshalCBOR(s.Blob)
}

// MarshalCBOR serializes the type into a CBOR byte vector.
func (s *Signed) MarshalCBOR() []byte {
	return cbor.Marshal(s)
}

// UnmarshalCBOR deserializes a CBOR byte vector into given type.
func (s *Signed) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, s)
}

// FromProto deserializes a protobuf into a Signed.
func (s *Signed) FromProto(pb *common.Signed) error {
	if pb == nil {
		return ErrNilProtobuf
	}

	s.Blob = pb.GetBlob()
	return s.Signature.FromProto(pb.GetSignature())
}

// ToProto serializes a protobuf version of the Signed.
func (s *Signed) ToProto() *common.Signed {
	return &common.Signed{
		Blob:      s.Blob,
		Signature: s.Signature.ToProto(),
	}
}

// SignedPublicKey is a signed blob containing a PublicKey.
type SignedPublicKey struct {
	Signed
}

// Open first verifies the blob signature and then unmarshals the blob.
func (s *SignedPublicKey) Open(context []byte, pub *PublicKey) error { // nolint: interfacer
	return s.Signed.Open(context, pub)
}

// VerifyManyToOne verifies multiple signatures against a single context and
// message, returning true iff every signature is valid.
func VerifyManyToOne(context []byte, message []byte, sigs []Signature) bool {
	// Our batch verify supports doing Ed25519ph/Ed25519ctx in bulk,
	// but we're stuck with this stupidity.
	msg, err := PrepareSignerMessage(context, message)
	if err != nil {
		return false
	}

	// Adapt from our wrapper types to the types used by the library.
	pks := make([]ed25519.PublicKey, 0, len(sigs))
	rawSigs := make([][]byte, 0, len(sigs))
	msgs := make([][]byte, 0, len(sigs))

	for i := range sigs {
		v := sigs[i] // This is deliberate.
		if v.PublicKey.isBlacklisted() {
			return false
		}

		pks = append(pks, ed25519.PublicKey(v.PublicKey))
		rawSigs = append(rawSigs, v.Signature[:])
		msgs = append(msgs, msg)
	}

	allOk, _, err := ed25519.VerifyBatch(rand.Reader, pks, msgs, rawSigs, defaultOptions)
	if err != nil {
		return false
	}

	return allOk
}

// VerifyBatch verifies multiple signatures, made by multiple public keys,
// against a single context and multiple messages, returning true iff every
// signature is valid.
func VerifyBatch(context []byte, messages [][]byte, sigs []Signature) bool {
	if len(messages) != len(sigs) {
		panic("signature: VerifyBatch messages/signature count mismatch")
	}

	// Adapt from our wrapper types to the types used by the library.
	pks := make([]ed25519.PublicKey, 0, len(sigs))
	rawSigs := make([][]byte, 0, len(sigs))
	msgs := make([][]byte, 0, len(sigs))

	for i := range sigs {
		v := sigs[i] // This is deliberate.
		if v.PublicKey.isBlacklisted() {
			return false
		}
		pks = append(pks, ed25519.PublicKey(v.PublicKey))
		rawSigs = append(rawSigs, v.Signature[:])

		// Sigh. :(
		msg, err := PrepareSignerMessage(context, messages[i])
		if err != nil {
			return false
		}
		msgs = append(msgs, msg)
	}

	allOk, _, err := ed25519.VerifyBatch(rand.Reader, pks, msgs, rawSigs, defaultOptions)
	if err != nil {
		return false
	}

	return allOk
}

// RegisterTestPublicKey registers a hardcoded test public key with the
// internal public key blacklist.
func RegisterTestPublicKey(pk PublicKey) {
	testPublicKeys.Store(pk.ToMapKey(), true)
}

// BuildPublicKeyBlacklist builds the public key blacklist.
func BuildPublicKeyBlacklist(allowTestKeys bool) {
	if !allowTestKeys {
		testPublicKeys.Range(func(k, v interface{}) bool {
			blacklistedPublicKeys.Store(k, v)
			return true
		})
	}

	// Explicitly forbid other keys here.
}
