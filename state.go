package main

import (
	"encoding/json"
	"os"
	"time"
)

const stateFileName = "motion_state.json"

type camState struct {
	Hash     uint64    `json:"hash"`
	LastSent time.Time `json:"last_sent"`
}

type motionState struct {
	Cameras map[string]camState `json:"cameras"`
}

func loadState() motionState {
	st := motionState{Cameras: make(map[string]camState)}
	data, err := os.ReadFile(statePath)
	if err == nil {
		json.Unmarshal(data, &st)
	}
	if st.Cameras == nil {
		st.Cameras = make(map[string]camState)
	}
	return st
}

func saveState(st motionState) {
	data, _ := json.Marshal(st)
	os.WriteFile(statePath, data, fileMode)
}
