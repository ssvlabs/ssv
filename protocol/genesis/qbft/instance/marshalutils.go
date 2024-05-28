package instance

import "encoding/json"

/////////////////////// JSON Marshalling for Tests ///////////////////////

// region: JSON Marshalling for Instance

// MarshalJSON is a custom JSON marshaller for Instance
func (i *Instance) MarshalJSON() ([]byte, error) {
	type Alias Instance
	if i.forceStop {
		return json.Marshal(&struct {
			ForceStop bool `json:"forceStop"`
			*Alias
		}{
			ForceStop: i.forceStop,
			Alias:     (*Alias)(i),
		})
	} else {
		return json.Marshal(&struct {
			*Alias
		}{
			Alias: (*Alias)(i),
		})
	}
}

// UnmarshalJSON is a custom JSON unmarshaller for Instance
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
		i.forceStop = *aux.ForceStop
	}
	return nil
}

// endregion: JSON Marshalling for Instance
