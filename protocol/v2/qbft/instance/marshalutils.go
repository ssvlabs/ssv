package instance

import "encoding/json"

/////////////////////// JSON Marshaling for Tests ///////////////////////

// region: JSON Marshaling for Instance

// MarshalJSON is a custom JSON marshaller for Instance
func (i *Instance) MarshalJSON() ([]byte, error) {
	type Alias Instance
	if i.markedIrrelevant {
		return json.Marshal(&struct {
			ForceStop bool `json:"forceStop"`
			*Alias
		}{
			ForceStop: i.markedIrrelevant,
			Alias:     (*Alias)(i),
		})
	}

	return json.Marshal(&struct {
		*Alias
	}{
		Alias: (*Alias)(i),
	})
}

// UnmarshalJSON overlays spec-test state; the receiver must already be constructed via NewInstance as i.roundTimer
// is not restored.
func (i *Instance) UnmarshalJSON(data []byte) error {
	type Alias Instance
	aux := &struct {
		ForceStop *bool `json:"forceStop,omitempty"`
		*Alias
	}{
		Alias: (*Alias)(i),
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	if aux.ForceStop != nil {
		i.markedIrrelevant = *aux.ForceStop
	}
	return nil
}

// endregion: JSON Marshaling for Instance
