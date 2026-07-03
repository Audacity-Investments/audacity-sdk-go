// Live smoke test: AUDACITY_API_KEY=… go run ./examples/smoke
package main

import (
	"context"
	"errors"
	"fmt"
	"log"

	audacity "github.com/Audacity-Investments/audacity-sdk-go"
	"github.com/Audacity-Investments/audacity-sdk-go/audacityruntime"
	"github.com/Audacity-Investments/audacity-sdk-go/audacityruntime/types"
)

func userMessage(text string) types.Message {
	return types.Message{
		Role:    types.ConversationRoleUser,
		Content: []types.ContentBlock{&types.ContentBlockMemberText{Value: text}},
	}
}

func main() {
	ctx := context.Background()
	client := audacityruntime.New(audacityruntime.Options{})

	fmt.Println("--- non-streaming ---")
	resp, err := client.Converse(ctx, &audacityruntime.ConverseInput{
		ModelId:  audacity.String("gpt-5.4-mini"),
		Messages: []types.Message{userMessage("Reply with exactly: OK")},
		InferenceConfig: &types.InferenceConfiguration{
			MaxTokens:   audacity.Int32(20),
			Temperature: audacity.Float32(0),
		},
	})
	if err != nil {
		log.Fatal(err)
	}
	msg := resp.Output.(*types.ConverseOutputMemberMessage)
	text := msg.Value.Content[0].(*types.ContentBlockMemberText).Value
	fmt.Printf("text: %q\n", text)
	fmt.Printf("stopReason: %s | usage: %d/%d/%d | latencyMs: %d\n",
		resp.StopReason, resp.Usage.InputTokens, resp.Usage.OutputTokens, resp.Usage.TotalTokens,
		resp.Metrics.LatencyMs)

	fmt.Println("--- streaming ---")
	streamResp, err := client.ConverseStream(ctx, &audacityruntime.ConverseStreamInput{
		ModelId:  audacity.String("gpt-5.4-mini"),
		Messages: []types.Message{userMessage("Count from 1 to 5, digits separated by spaces.")},
		InferenceConfig: &types.InferenceConfiguration{
			MaxTokens: audacity.Int32(30),
		},
	})
	if err != nil {
		log.Fatal(err)
	}
	stream := streamResp.GetStream()
	var kinds []string
	addKind := func(k string) {
		if len(kinds) == 0 || kinds[len(kinds)-1] != k {
			kinds = append(kinds, k)
		}
	}
	var streamed string
	for event := range stream.Events() {
		switch e := event.(type) {
		case *types.ConverseStreamOutputMemberMessageStart:
			addKind("messageStart")
		case *types.ConverseStreamOutputMemberContentBlockStart:
			addKind("contentBlockStart")
		case *types.ConverseStreamOutputMemberContentBlockDelta:
			addKind("contentBlockDelta")
			if d, ok := e.Value.Delta.(*types.ContentBlockDeltaMemberText); ok {
				streamed += d.Value
			}
		case *types.ConverseStreamOutputMemberContentBlockStop:
			addKind("contentBlockStop")
		case *types.ConverseStreamOutputMemberMessageStop:
			addKind("messageStop")
		case *types.ConverseStreamOutputMemberMetadata:
			addKind("metadata")
			fmt.Printf("metadata usage: %d/%d/%d\n",
				e.Value.Usage.InputTokens, e.Value.Usage.OutputTokens, e.Value.Usage.TotalTokens)
		}
	}
	if err := stream.Err(); err != nil {
		log.Fatal(err)
	}
	_ = stream.Close()
	fmt.Printf("event kinds in order: %v\n", kinds)
	fmt.Printf("streamed text: %q\n", streamed)

	fmt.Println("--- error: bad api key ---")
	badClient := audacityruntime.New(audacityruntime.Options{APIKey: "audacity_api_bogus"})
	_, err = badClient.Converse(ctx, &audacityruntime.ConverseInput{
		ModelId:  audacity.String("gpt-5.4-mini"),
		Messages: []types.Message{userMessage("hi")},
	})
	var accessDenied *types.AccessDeniedException
	if errors.As(err, &accessDenied) {
		fmt.Printf("AccessDeniedException: %d %s %s\n",
			accessDenied.StatusCode, accessDenied.ErrorCode, accessDenied.Message)
	} else {
		fmt.Printf("FAIL: expected AccessDeniedException, got %v\n", err)
	}

	fmt.Println("--- error: bad model ---")
	_, err = client.Converse(ctx, &audacityruntime.ConverseInput{
		ModelId:  audacity.String("not-a-real-model-xyz"),
		Messages: []types.Message{userMessage("hi")},
	})
	var validation *types.ValidationException
	if errors.As(err, &validation) {
		requestID := ""
		if validation.RequestID != nil {
			requestID = *validation.RequestID
		}
		fmt.Printf("ValidationException: %d %s requestId=%s\n",
			validation.StatusCode, validation.ErrorCode, requestID)
	} else {
		fmt.Printf("FAIL: expected ValidationException, got %v\n", err)
	}
}
