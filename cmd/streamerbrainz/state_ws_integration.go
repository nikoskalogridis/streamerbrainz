package main

// This file is intentionally left empty.
//
// The state WebSocket implementation now lives directly in the main package
// (see `state_ws.go`) and is wired from `main.go` without any adapter layer.
// Keeping this file (as an empty stub) avoids breaking references in older
// branches/patches and makes the removal explicit in the repository history.
