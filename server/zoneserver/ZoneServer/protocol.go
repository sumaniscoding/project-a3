package main

type ClientMessage struct {
	Command string      `json:"command"`
	Payload interface{} `json:"payload"`
}

type ServerMessage struct {
	Command string      `json:"command"`
	Payload interface{} `json:"payload"`
}

