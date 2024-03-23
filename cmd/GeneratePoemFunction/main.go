package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/go-micah/clevelandart"
	"github.com/go-micah/go-bedrock/providers"
	"github.com/go-micah/go-generate-poems/poems"
)

type Response struct {
	Poem            string `json:"poem,omitempty"`
	Id              string `json:"id,omitempty"`
	AccessionNumber string `json:"accessionNumber,omitempty"`
	Error           string `json:"error,omitempty"`
}

func CraftPrompt(doc string) string {
	// craft a prompt with the artwork
	document := "<document>" + doc + "</document>\n\n"

	prompt := document + "Write a short poem inspired by the artwork described by the <document>"

	return prompt
}

func SendPromptToBedrock(prompt string) (string, error) {
	// send prompt to Amazon Bedrock
	cfg, err := config.LoadDefaultConfig(context.TODO(), config.WithRegion("us-east-1"))
	if err != nil {
		return "", fmt.Errorf("unable to load AWS config: %v", err)
	}

	svc := bedrockruntime.NewFromConfig(cfg)

	accept := "*/*"
	contentType := "application/json"
	modelId := "anthropic.claude-3-haiku-20240307-v1:0"

	body := providers.AnthropicClaudeMessagesInvokeModelInput{
		System: "Respond with just the poem, nothing else.",
		Messages: []providers.AnthropicClaudeMessage{
			{
				Role: "user",
				Content: []providers.AnthropicClaudeContent{
					{
						Type: "text",
						Text: prompt,
					},
				},
			},
		},
		MaxTokens:     500,
		TopP:          0.999,
		TopK:          250,
		Temperature:   1,
		StopSequences: []string{},
	}

	bodyString, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("unable to marshal body: %v", err)
	}

	bedrockResp, err := svc.InvokeModel(context.TODO(), &bedrockruntime.InvokeModelInput{
		Accept:      &accept,
		ModelId:     &modelId,
		ContentType: &contentType,
		Body:        bodyString,
	})
	if err != nil {
		return "", fmt.Errorf("error from Bedrock, %v", err)
	}

	var out providers.AnthropicClaudeMessagesInvokeModelOutput

	err = json.Unmarshal(bedrockResp.Body, &out)
	if err != nil {
		return "", fmt.Errorf("unable to unmarshal response from Bedrock: %v", err)
	}

	return out.Content[0].Text, nil

}

func handler(request events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {

	// grab id from querystring for an artwork ID or Accession number 160729
	id := request.QueryStringParameters["id"]

	// check if we already have a poem in the database
	poem, err := poems.GetPoem(id)
	if err != nil {
		fmt.Println(err)
		resp := Response{
			Error: err.Error(),
		}
		jsonResp, _ := json.Marshal(resp)
		return events.APIGatewayProxyResponse{
			Body:       string(jsonResp),
			StatusCode: 500,
		}, nil
	}

	// if we have a poem - return it to API GW
	if poem.ID != "" {
		fmt.Println("we already have the poem")
		resp := Response{
			Poem:            poem.Poem,
			Id:              poem.ID,
			AccessionNumber: poem.AccessionNumber,
		}
		jsonResp, _ := json.Marshal(resp)
		return events.APIGatewayProxyResponse{
			Body:       string(jsonResp),
			StatusCode: 200,
		}, nil
	}

	// get artwork from CMA
	art, err := clevelandart.GetArtwork(id)
	if err != nil {
		fmt.Println(err)
		resp := Response{
			Error: err.Error(),
		}
		jsonResp, _ := json.Marshal(resp)
		return events.APIGatewayProxyResponse{
			Body:       string(jsonResp),
			StatusCode: 500,
		}, nil
	}

	// craft prompt
	prompt := CraftPrompt(art.JSON)

	// send prompt to Bedrock
	newPoem, err := SendPromptToBedrock(prompt)
	if err != nil {
		fmt.Println(err)
		resp := Response{
			Error: err.Error(),
		}
		jsonResp, _ := json.Marshal(resp)
		return events.APIGatewayProxyResponse{
			Body:       string(jsonResp),
			StatusCode: 500,
		}, nil
	}

	// write poem to database
	var p poems.Poem
	p.ID = fmt.Sprint(art.Id)
	p.Poem = newPoem
	p.AccessionNumber = art.AccessionNumber

	err = poems.PutPoem(p)
	if err != nil {
		fmt.Println(err)
		resp := Response{
			Error: err.Error(),
		}
		jsonResp, _ := json.Marshal(resp)
		return events.APIGatewayProxyResponse{
			Body:       string(jsonResp),
			StatusCode: 500,
		}, nil
	}

	// return the poem
	resp := Response{
		Poem:            newPoem,
		Id:              fmt.Sprint(art.Id),
		AccessionNumber: art.AccessionNumber,
	}
	jsonResp, _ := json.Marshal(resp)
	return events.APIGatewayProxyResponse{
		Body:       string(jsonResp),
		StatusCode: 200,
	}, nil

}

func main() {
	lambda.Start(handler)
}
