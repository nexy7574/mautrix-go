package ratchet

import (
	"fmt"

	"maunium.net/go/mautrix/crypto/goolm/crypto"
	"maunium.net/go/mautrix/crypto/goolm/libolmpickle"
	"maunium.net/go/mautrix/crypto/olm"
)

const (
	chainKeySeed     = 0x02
	messageKeyLength = 32
)

// chainKey wraps the index and the public key
type chainKey struct {
	Index uint32                     `json:"index"`
	Key   crypto.Curve25519PublicKey `json:"key"`
}

const chainKeyPickleLength = crypto.Curve25519PubKeyLength + // Key
	libolmpickle.PickleUInt32Length // Index

// advance advances the chain
func (c *chainKey) advance() {
	c.Key = crypto.HMACSHA256(c.Key, []byte{chainKeySeed})
	c.Index++
}

// UnpickleLibOlm decodes the unencryted value and populates the chain key accordingly. It returns the number of bytes read.
func (r *chainKey) UnpickleLibOlm(value []byte) (int, error) {
	curPos := 0
	readBytes, err := r.Key.UnpickleLibOlm(value)
	if err != nil {
		return 0, err
	}
	curPos += readBytes
	r.Index, readBytes, err = libolmpickle.UnpickleUInt32(value[curPos:])
	if err != nil {
		return 0, err
	}
	curPos += readBytes
	return curPos, nil
}

// PickleLibOlm encodes the chain key into target. target has to have a size of at least PickleLen() and is written to from index 0.
// It returns the number of bytes written.
func (r chainKey) PickleLibOlm(target []byte) (int, error) {
	if len(target) < chainKeyPickleLength {
		return 0, fmt.Errorf("pickle chain key: %w", olm.ErrValueTooShort)
	}
	written, err := r.Key.PickleLibOlm(target)
	if err != nil {
		return 0, fmt.Errorf("pickle chain key: %w", err)
	}
	written += libolmpickle.PickleUInt32(r.Index, target[written:])
	return written, nil
}

// senderChain is a chain for sending messages
type senderChain struct {
	RKey  crypto.Curve25519KeyPair `json:"ratchet_key"`
	CKey  chainKey                 `json:"chain_key"`
	IsSet bool                     `json:"set"`
}

const senderChainPickleLength = chainKeyPickleLength + // RKey
	chainKeyPickleLength // CKey

// newSenderChain returns a sender chain initialized with chainKey and ratchet key pair.
func newSenderChain(key crypto.Curve25519PublicKey, ratchet crypto.Curve25519KeyPair) *senderChain {
	return &senderChain{
		RKey: ratchet,
		CKey: chainKey{
			Index: 0,
			Key:   key,
		},
		IsSet: true,
	}
}

// advance advances the chain
func (s *senderChain) advance() {
	s.CKey.advance()
}

// ratchetKey returns the ratchet key pair.
func (s senderChain) ratchetKey() crypto.Curve25519KeyPair {
	return s.RKey
}

// chainKey returns the current chainKey.
func (s senderChain) chainKey() chainKey {
	return s.CKey
}

// UnpickleLibOlm decodes the unencryted value and populates the chain accordingly. It returns the number of bytes read.
func (r *senderChain) UnpickleLibOlm(value []byte) (int, error) {
	curPos := 0
	readBytes, err := r.RKey.UnpickleLibOlm(value)
	if err != nil {
		return 0, err
	}
	curPos += readBytes
	readBytes, err = r.CKey.UnpickleLibOlm(value[curPos:])
	if err != nil {
		return 0, err
	}
	curPos += readBytes
	return curPos, nil
}

// PickleLibOlm encodes the chain into target. target has to have a size of at least PickleLen() and is written to from index 0.
// It returns the number of bytes written.
func (r senderChain) PickleLibOlm(target []byte) (int, error) {
	if len(target) < senderChainPickleLength {
		return 0, fmt.Errorf("pickle sender chain: %w", olm.ErrValueTooShort)
	}
	written, err := r.RKey.PickleLibOlm(target)
	if err != nil {
		return 0, fmt.Errorf("pickle sender chain: %w", err)
	}
	writtenChain, err := r.CKey.PickleLibOlm(target[written:])
	if err != nil {
		return 0, fmt.Errorf("pickle sender chain: %w", err)
	}
	written += writtenChain
	return written, nil
}

// senderChain is a chain for receiving messages
type receiverChain struct {
	RKey crypto.Curve25519PublicKey `json:"ratchet_key"`
	CKey chainKey                   `json:"chain_key"`
}

const receiverChainPickleLength = crypto.Curve25519PubKeyLength + // Ratchet Key
	chainKeyPickleLength // CKey

// newReceiverChain returns a receiver chain initialized with chainKey and ratchet public key.
func newReceiverChain(chain crypto.Curve25519PublicKey, ratchet crypto.Curve25519PublicKey) *receiverChain {
	return &receiverChain{
		RKey: ratchet,
		CKey: chainKey{
			Index: 0,
			Key:   chain,
		},
	}
}

// advance advances the chain
func (s *receiverChain) advance() {
	s.CKey.advance()
}

// ratchetKey returns the ratchet public key.
func (s receiverChain) ratchetKey() crypto.Curve25519PublicKey {
	return s.RKey
}

// chainKey returns the current chainKey.
func (s receiverChain) chainKey() chainKey {
	return s.CKey
}

// UnpickleLibOlm decodes the unencryted value and populates the chain accordingly. It returns the number of bytes read.
func (r *receiverChain) UnpickleLibOlm(value []byte) (int, error) {
	curPos := 0
	readBytes, err := r.RKey.UnpickleLibOlm(value)
	if err != nil {
		return 0, err
	}
	curPos += readBytes
	readBytes, err = r.CKey.UnpickleLibOlm(value[curPos:])
	if err != nil {
		return 0, err
	}
	curPos += readBytes
	return curPos, nil
}

// PickleLibOlm encodes the chain into target. target has to have a size of at least PickleLen() and is written to from index 0.
// It returns the number of bytes written.
func (r receiverChain) PickleLibOlm(target []byte) (int, error) {
	if len(target) < receiverChainPickleLength {
		return 0, fmt.Errorf("pickle sender chain: %w", olm.ErrValueTooShort)
	}
	written, err := r.RKey.PickleLibOlm(target)
	if err != nil {
		return 0, fmt.Errorf("pickle sender chain: %w", err)
	}
	writtenChain, err := r.CKey.PickleLibOlm(target)
	if err != nil {
		return 0, fmt.Errorf("pickle sender chain: %w", err)
	}
	written += writtenChain
	return written, nil
}

// messageKey wraps the index and the key of a message
type messageKey struct {
	Index uint32 `json:"index"`
	Key   []byte `json:"key"`
}

const messageKeyPickleLength = messageKeyLength + // Key
	libolmpickle.PickleUInt32Length // Index

// UnpickleLibOlm decodes the unencryted value and populates the message key accordingly. It returns the number of bytes read.
func (m *messageKey) UnpickleLibOlm(value []byte) (int, error) {
	curPos := 0
	ratchetKey, readBytes, err := libolmpickle.UnpickleBytes(value, messageKeyLength)
	if err != nil {
		return 0, err
	}
	m.Key = ratchetKey
	curPos += readBytes
	keyID, readBytes, err := libolmpickle.UnpickleUInt32(value[:curPos])
	if err != nil {
		return 0, err
	}
	curPos += readBytes
	m.Index = keyID
	return curPos, nil
}

// PickleLibOlm encodes the message key into target. target has to have a size of at least PickleLen() and is written to from index 0.
// It returns the number of bytes written.
func (m messageKey) PickleLibOlm(target []byte) (int, error) {
	if len(target) < messageKeyPickleLength {
		return 0, fmt.Errorf("pickle message key: %w", olm.ErrValueTooShort)
	}
	written := 0
	if len(m.Key) != messageKeyLength {
		written += libolmpickle.PickleBytes(make([]byte, messageKeyLength), target)
	} else {
		written += libolmpickle.PickleBytes(m.Key, target)
	}
	written += libolmpickle.PickleUInt32(m.Index, target[written:])
	return written, nil
}
