package crypto

import (
	"context"
	"encoding/base64"
	"fmt"
	"time"

	"github.com/rs/zerolog"

	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/crypto/backup"
	"maunium.net/go/mautrix/crypto/olm"
	"maunium.net/go/mautrix/crypto/signatures"
	"maunium.net/go/mautrix/id"
)

func (mach *OlmMachine) DownloadAndStoreLatestKeyBackup(ctx context.Context, megolmBackupKey *backup.MegolmBackupKey) (id.KeyBackupVersion, error) {
	log := mach.machOrContextLog(ctx).With().
		Str("action", "download and store latest key backup").
		Logger()

	ctx = log.WithContext(ctx)

	versionInfo, err := mach.GetAndVerifyLatestKeyBackupVersion(ctx, megolmBackupKey)
	if err != nil {
		return "", err
	} else if versionInfo == nil {
		return "", nil
	}

	err = mach.GetAndStoreKeyBackup(ctx, versionInfo.Version, megolmBackupKey)
	return versionInfo.Version, err
}

func (mach *OlmMachine) GetAndVerifyLatestKeyBackupVersion(ctx context.Context, megolmBackupKey *backup.MegolmBackupKey) (*mautrix.RespRoomKeysVersion[backup.MegolmAuthData], error) {
	versionInfo, err := mach.Client.GetKeyBackupLatestVersion(ctx)
	if err != nil {
		return nil, err
	}

	if versionInfo.Algorithm != id.KeyBackupAlgorithmMegolmBackupV1 {
		return nil, fmt.Errorf("unsupported key backup algorithm: %s", versionInfo.Algorithm)
	}

	log := mach.machOrContextLog(ctx).With().
		Int("count", versionInfo.Count).
		Str("etag", versionInfo.ETag).
		Stringer("key_backup_version", versionInfo.Version).
		Logger()

	// https://spec.matrix.org/v1.10/client-server-api/#server-side-key-backups
	// "Clients must only store keys in backups after they have ensured that the auth_data is trusted. This can be done either...
	// ...by deriving the public key from a private key that it obtained from a trusted source. Trusted sources for the private
	// key include the user entering the key, retrieving the key stored in secret storage, or obtaining the key via secret sharing
	// from a verified device belonging to the same user."
	megolmBackupDerivedPublicKey := id.Ed25519(base64.RawStdEncoding.EncodeToString(megolmBackupKey.PublicKey().Bytes()))
	if megolmBackupKey != nil && versionInfo.AuthData.PublicKey == megolmBackupDerivedPublicKey {
		log.Debug().Msg("key backup is trusted based on derived public key")
		return versionInfo, nil
	} else {
		log.Debug().
			Stringer("expected_key", megolmBackupDerivedPublicKey).
			Stringer("actual_key", versionInfo.AuthData.PublicKey).
			Msg("key backup public keys do not match, proceeding to check device signatures")
	}

	// "...or checking that it is signed by the user’s master cross-signing key or by a verified device belonging to the same user"
	userSignatures, ok := versionInfo.AuthData.Signatures[mach.Client.UserID]
	if !ok {
		return nil, fmt.Errorf("no signature from user %s found in key backup", mach.Client.UserID)
	}

	crossSigningPubkeys := mach.GetOwnCrossSigningPublicKeys(ctx)
	if crossSigningPubkeys == nil {
		return nil, ErrCrossSigningPubkeysNotCached
	}

	signatureVerified := false
	for keyID := range userSignatures {
		keyAlg, keyName := keyID.Parse()
		if keyAlg != id.KeyAlgorithmEd25519 {
			continue
		}
		log := log.With().Str("key_name", keyName).Logger()

		var key id.Ed25519
		if keyName == crossSigningPubkeys.MasterKey.String() {
			key = crossSigningPubkeys.MasterKey
		} else if device, err := mach.CryptoStore.GetDevice(ctx, mach.Client.UserID, id.DeviceID(keyName)); err != nil {
			return nil, fmt.Errorf("failed to get device %s/%s from store: %w", mach.Client.UserID, keyName, err)
		} else if device == nil {
			log.Warn().Err(err).Msg("Device does not exist, ignoring signature")
			continue
		} else if !mach.IsDeviceTrusted(ctx, device) {
			log.Warn().Err(err).Msg("Device is not trusted")
			continue
		} else {
			key = device.SigningKey
		}

		ok, err = signatures.VerifySignatureJSON(versionInfo.AuthData, mach.Client.UserID, keyName, key)
		if err != nil || !ok {
			log.Warn().Err(err).Stringer("key_id", keyID).Msg("Signature verification failed")
			continue
		} else {
			// One of the signatures is valid, break from the loop.
			log.Debug().Stringer("key_id", keyID).Msg("key backup is trusted based on matching signature")
			signatureVerified = true
			break
		}
	}
	if !signatureVerified {
		return nil, fmt.Errorf("no valid signature from user %s found in key backup", mach.Client.UserID)
	}

	return versionInfo, nil
}

func (mach *OlmMachine) GetAndStoreKeyBackup(ctx context.Context, version id.KeyBackupVersion, megolmBackupKey *backup.MegolmBackupKey) error {
	keys, err := mach.Client.GetKeyBackup(ctx, version)
	if err != nil {
		return err
	}

	log := zerolog.Ctx(ctx)

	var count, failedCount int

	for roomID, backup := range keys.Rooms {
		for sessionID, keyBackupData := range backup.Sessions {
			sessionData, err := keyBackupData.SessionData.Decrypt(megolmBackupKey)
			if err != nil {
				log.Warn().Err(err).Msg("Failed to decrypt session data")
				failedCount++
				continue
			}

			_, err = mach.ImportRoomKeyFromBackup(ctx, version, roomID, sessionID, sessionData)
			if err != nil {
				log.Warn().Err(err).Msg("Failed to import room key from backup")
				failedCount++
				continue
			}
			count++
		}
	}

	log.Info().
		Int("count", count).
		Int("failed_count", failedCount).
		Msg("successfully imported sessions from backup")

	return nil
}

func (mach *OlmMachine) ImportRoomKeyFromBackup(ctx context.Context, version id.KeyBackupVersion, roomID id.RoomID, sessionID id.SessionID, keyBackupData *backup.MegolmSessionData) (*InboundGroupSession, error) {
	log := zerolog.Ctx(ctx).With().
		Str("room_id", roomID.String()).
		Str("session_id", sessionID.String()).
		Logger()
	if keyBackupData.Algorithm != id.AlgorithmMegolmV1 {
		return nil, fmt.Errorf("ignoring room key in backup with weird algorithm %s", keyBackupData.Algorithm)
	}

	igsInternal, err := olm.InboundGroupSessionImport([]byte(keyBackupData.SessionKey))
	if err != nil {
		return nil, fmt.Errorf("failed to import inbound group session: %w", err)
	} else if igsInternal.ID() != sessionID {
		log.Warn().
			Stringer("actual_session_id", igsInternal.ID()).
			Msg("Mismatched session ID while creating inbound group session from key backup")
		return nil, fmt.Errorf("mismatched session ID while creating inbound group session from key backup")
	}

	var maxAge time.Duration
	var maxMessages int
	if config, err := mach.StateStore.GetEncryptionEvent(ctx, roomID); err != nil {
		log.Error().Err(err).Msg("Failed to get encryption event for room")
	} else if config != nil {
		maxAge = time.Duration(config.RotationPeriodMillis) * time.Millisecond
		maxMessages = config.RotationPeriodMessages
	}

	firstKnownIndex := igsInternal.FirstKnownIndex()
	if firstKnownIndex > 0 {
		log.Warn().Uint32("first_known_index", firstKnownIndex).Msg("Importing partial session")
	}

	igs := &InboundGroupSession{
		Internal:         igsInternal,
		SigningKey:       keyBackupData.SenderClaimedKeys.Ed25519,
		SenderKey:        keyBackupData.SenderKey,
		RoomID:           roomID,
		ForwardingChains: append(keyBackupData.ForwardingKeyChain, keyBackupData.SenderKey.String()),
		id:               sessionID,

		ReceivedAt:       time.Now().UTC(),
		MaxAge:           maxAge.Milliseconds(),
		MaxMessages:      maxMessages,
		KeyBackupVersion: version,
	}
	err = mach.CryptoStore.PutGroupSession(ctx, igs)
	if err != nil {
		return nil, fmt.Errorf("failed to store new inbound group session: %w", err)
	}
	mach.markSessionReceived(ctx, roomID, sessionID, firstKnownIndex)
	return igs, nil
}
