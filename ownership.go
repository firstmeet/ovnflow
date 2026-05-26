package ovnflow

import (
	"encoding/base64"
	"strings"
)

const (
	ExternalIDPrefix = "ovnflow.io/"

	ExternalIDManagedByKey  = ExternalIDPrefix + "managed-by"
	ExternalIDAPIVersionKey = ExternalIDPrefix + "api-version"
	ExternalIDKindKey       = ExternalIDPrefix + "kind"
	ExternalIDNameKey       = ExternalIDPrefix + "name"
	ExternalIDOwnerKindKey  = ExternalIDPrefix + "owner-kind"
	ExternalIDOwnerNameKey  = ExternalIDPrefix + "owner-name"
	ExternalIDOwnerIDKey    = ExternalIDPrefix + "owner-id"
	ExternalIDLabelPrefix   = ExternalIDPrefix + "label/"
)

// OwnerRef identifies the neutral controller or platform object that owns an
// ovnflow-managed resource. Kind is required; either Name or ID must be set.
type OwnerRef struct {
	Kind string
	Name string
	ID   string
}

// Labels carries portable grouping metadata without embedding private platform
// concepts in the SDK.
type Labels map[string]string

func (o OwnerRef) Validate() error {
	if strings.TrimSpace(o.Kind) == "" {
		return wrap(ErrorValidation, "", "", "validate", "owner", "owner kind must not be empty", nil)
	}
	if strings.TrimSpace(o.Name) == "" && strings.TrimSpace(o.ID) == "" {
		return wrap(ErrorValidation, "", "", "validate", "owner", "owner name or id is required", nil)
	}
	return nil
}

func (l Labels) Validate() error {
	for key := range l {
		if strings.TrimSpace(key) == "" {
			return wrap(ErrorValidation, "", "", "validate", "labels", "label key must not be empty", nil)
		}
	}
	return nil
}

// ExternalIDs returns reserved ovnflow external_ids for ownership and labels.
func (o OwnerRef) ExternalIDs(labels Labels) (map[string]string, error) {
	if err := o.Validate(); err != nil {
		return nil, err
	}
	if err := labels.Validate(); err != nil {
		return nil, err
	}
	out := map[string]string{
		ExternalIDManagedByKey:  "ovnflow",
		ExternalIDAPIVersionKey: "v2",
		ExternalIDOwnerKindKey:  o.Kind,
	}
	if o.Name != "" {
		out[ExternalIDOwnerNameKey] = o.Name
	}
	if o.ID != "" {
		out[ExternalIDOwnerIDKey] = o.ID
	}
	for key, value := range labels {
		out[ExternalIDLabelKey(key)] = value
	}
	return out, nil
}

// ExternalIDLabelKey encodes an arbitrary label key as an external_ids key.
// The suffix is raw URL-safe base64 without padding.
func ExternalIDLabelKey(labelKey string) string {
	return ExternalIDLabelPrefix + base64.RawURLEncoding.EncodeToString([]byte(labelKey))
}

func DecodeExternalIDLabelKey(externalIDKey string) (string, bool) {
	if !strings.HasPrefix(externalIDKey, ExternalIDLabelPrefix) {
		return "", false
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(externalIDKey, ExternalIDLabelPrefix))
	if err != nil {
		return "", false
	}
	return string(raw), true
}

func IsReservedExternalIDKey(key string) bool {
	return strings.HasPrefix(key, ExternalIDPrefix)
}

func OwnershipMatches(externalIDs map[string]string, owner OwnerRef) bool {
	if err := owner.Validate(); err != nil {
		return false
	}
	if externalIDs[ExternalIDOwnerKindKey] != owner.Kind {
		return false
	}
	if owner.Name != "" && externalIDs[ExternalIDOwnerNameKey] != owner.Name {
		return false
	}
	if owner.ID != "" && externalIDs[ExternalIDOwnerIDKey] != owner.ID {
		return false
	}
	return true
}
