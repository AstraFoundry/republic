// Run with REPUBLIC_OPENAI_API_KEY set. Tools are never executed here.
package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/AstraFoundry/republic"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	model, err := republic.GetModel("openai:gpt-6-sol", republic.ProviderConfig{})
	if err != nil {
		log.Fatal(err)
	}
	defer model.Close()
	stream, err := model.Stream(ctx, "Explain how an agent uses a tool result.", nil)
	if err != nil {
		log.Fatal(err)
	}
	defer stream.Close()
	for event, err := range stream.Events() {
		if err != nil {
			log.Print(err)
			return
		}
		if event.Type == "text_delta" {
			fmt.Print(event.Chunk)
		}
	}
	response, err := stream.Response()
	if err != nil {
		log.Print(err)
		return
	}
	fmt.Printf("\n%d tokens\n", response.TokenUsage.TotalTokens())
}
