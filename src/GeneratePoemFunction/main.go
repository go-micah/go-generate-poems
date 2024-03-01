package main

import (
	"context"
	"encoding/json"
	"fmt"
	"go-generate-poems/clevelandart"
	"os"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

type Response struct {
	Poem  string `json:"poem"`
	Error string `json:"error"`
}

type Poem struct {
	ID   string `dynamodbav:"id"`
	Poem string `dynamodbav:"poem"`
}

type Body struct {
	Prompt           string   `json:"prompt"`
	MaxTokens        int      `json:"max_tokens_to_sample"`
	Temperature      int      `json:"temperature"`
	TopK             int      `json:"top_k"`
	TopP             float64  `json:"top_p"`
	StopSequences    []string `json:"stop_sequences"`
	AnthropicVersion string   `json:"anthropic_version"`
}

type AnthropicResponseBody struct {
	Completion string `json:"completion"`
	StopReason string `json:"stop_reason"`
}

func CraftPrompt(doc []byte) string {
	// craft a prompt with the artwork
	document := "<document>" + string(doc) + "</document>\n\n"

	prompt := "\n\nHuman: " + document + "Write a short poem inspired by the artwork described by the <document> \n\nAssistant:"

	return prompt
}

func PutPoem(poem Poem) error {
	cfg, err := config.LoadDefaultConfig(context.TODO(), config.WithRegion("us-east-1"))
	if err != nil {
		return fmt.Errorf("unable to load AWS config: %v", err)
	}

	svc := dynamodb.NewFromConfig(cfg)

	// POEMS_TABLE_NAME
	table := os.Getenv("POEMS_TABLE_NAME")
	if table == "" {
		return fmt.Errorf("POEMS_TABLE_NAME not set")
	}

	_, err = svc.PutItem(context.TODO(), &dynamodb.PutItemInput{
		TableName: &table,
		Item: map[string]types.AttributeValue{
			"id":   &types.AttributeValueMemberS{Value: poem.ID},
			"poem": &types.AttributeValueMemberS{Value: poem.Poem},
		},
	})
	if err != nil {
		return fmt.Errorf("could not write poem to the database %v", err)
	}

	return nil
}

func GetPoem(id string) (*Poem, error) {

	cfg, err := config.LoadDefaultConfig(context.TODO(), config.WithRegion("us-east-1"))
	if err != nil {
		return nil, fmt.Errorf("unable to load AWS config: %v", err)
	}

	svc := dynamodb.NewFromConfig(cfg)

	// POEMS_TABLE_NAME
	table := os.Getenv("POEMS_TABLE_NAME")
	if table == "" {
		return nil, fmt.Errorf("POEMS_TABLE_NAME not set")
	}

	// get poem from database
	poem, err := svc.GetItem(context.TODO(), &dynamodb.GetItemInput{
		TableName: &table,
		Key: map[string]types.AttributeValue{
			"id": &types.AttributeValueMemberS{Value: id},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("error talking to the database: %v", err)
	}

	var item Poem
	err = attributevalue.UnmarshalMap(poem.Item, &item)
	if err != nil {
		return nil, fmt.Errorf("error unmarshalling poem from database: %v", err)
	}

	return &item, nil
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
	modelId := "anthropic.claude-instant-v1"

	var body Body

	body.Prompt = prompt
	body.MaxTokens = 300
	body.Temperature = 1
	body.TopK = 250
	body.TopP = 0.999
	body.StopSequences = []string{
		"\n\nHuman:",
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

	var out AnthropicResponseBody

	err = json.Unmarshal(bedrockResp.Body, &out)
	if err != nil {
		return "", fmt.Errorf("unable to unmarshal response from Bedrock: %v", err)
	}

	return out.Completion, nil

}

func handler(request events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {

	// grab id from querystring for an artwork ID or Accession number 160729
	id := request.QueryStringParameters["id"]

	// check if we already have a poem in the database
	poem, err := GetPoem(id)
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
			Poem: poem.Poem,
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
	prompt := CraftPrompt(art)

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

	// log the poem
	//fmt.Println(newPoem)

	// write poem to database
	var p Poem
	p.ID = id
	p.Poem = newPoem
	err = PutPoem(p)
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
		Poem: newPoem,
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
