package main

import (
	"log"
	"os"

	"github.com/gammazero/nexus/client"
	"github.com/gammazero/nexus/transport/serialize"
)

type message struct {
	From, Message string
}

func main() {
	if len(os.Args) != 2 {
		log.Fatalf("Usage: %s <username>", os.Args[0])
	}

	if fi, err := os.Stderr.Stat(); err != nil {
		log.Fatal("Error checking stderr:", err)
	} else if fi.Mode()&os.ModeDevice != 0 {
		stderr, err := os.Open(os.DevNull)
		if err != nil {
			log.Fatal("Error redirecting stderr")
		}
		log.SetOutput(stderr)
	}

	username := os.Args[1]

	cliLogger := log.New(os.Stdout, "CLIENT> ", log.LstdFlags)
	c, err := client.NewWebsocketClient("ws://localhost:8000/", serialize.JSON, nil, nil, 0, cliLogger)
	if err != nil {
		log.Fatal(err)
	}
	_, err = c.JoinRealm("nexus.examples.chat", nil, nil)
	if err != nil {
		log.Fatal(err)
	}

	messages := make(chan message)
	err = c.Subscribe("chat", func(args []interface{}, kwargs map[string]interface{}, details map[string]interface{}) {
		if len(args) == 2 {
			if from, ok := args[0].(string); !ok {
				log.Println("First argument not a string:", args[0])
			} else if msg, ok := args[1].(string); !ok {
				log.Println("Second argument not a string:", args[1])
			} else {
				log.Printf("%s: %s", from, msg)
				messages <- message{From: from, Message: msg}
			}
		}
	}, nil)
	if err != nil {
		log.Fatalln("Error subscribing to chat channel:", err)
	}

	outgoing := make(chan message, 1)
	go sendMessages(c, outgoing)
	cw := newChatWin(username, outgoing)
	cw.dialog(messages)
}

func sendMessages(c *client.Client, messages chan message) {
	for msg := range messages {
		err := c.Publish("chat", nil, []interface{}{msg.From, msg.Message}, nil)
		if err != nil {
			log.Println("Error sending message:", err)
		}
	}
}
