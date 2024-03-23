package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/expression"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/go-micah/clevelandart"
	"github.com/go-micah/go-bedrock/providers"
)

type Response struct {
	Poem  string `json:"poem,omitempty"`
	Error string `json:"error,omitempty"`
}

type Poem struct {
	ID              string `dynamodbav:"id"`
	AccessionNumber string `dynamodbav:"accession_number"`
	Poem            string `dynamodbav:"poem"`
}

func CraftPrompt(doc string) string {
	// craft a prompt with the artwork
	document := "<document>" + doc + "</document>\n\n"

	prompt := document + "Write a short poem inspired by the artwork described by the <document>"

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
			"id":               &types.AttributeValueMemberS{Value: poem.ID},
			"accession_number": &types.AttributeValueMemberS{Value: poem.AccessionNumber},
			"poem":             &types.AttributeValueMemberS{Value: poem.Poem},
		},
	})
	if err != nil {
		return fmt.Errorf("could not write poem to the database %v", err)
	}

	return nil
}

func GetPoem(id string) (*Poem, error) {

	var accessionNumber string

	if _, err := strconv.Atoi(id); err != nil {
		accessionNumber = id
	}

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

	var poem *dynamodb.GetItemOutput
	var data *dynamodb.QueryOutput

	var item Poem
	var items []Poem

	index := "AccessionNumberIndex"

	filter := expression.Name("accession_number").Equal(expression.Value(accessionNumber))
	expr, _ := expression.NewBuilder().WithFilter(filter).Build()

	if accessionNumber != "" {
		data, err = svc.Query(context.TODO(), &dynamodb.QueryInput{
			TableName:                 &table,
			IndexName:                 &index,
			ExpressionAttributeNames:  expr.Names(),
			ExpressionAttributeValues: expr.Values(),
			KeyConditionExpression:    expr.Filter(),
		})
		if err != nil {
			return nil, fmt.Errorf("error talking to the database: %v", err)
		}

		err = attributevalue.UnmarshalListOfMaps(data.Items, &items)
		if err != nil {
			return nil, fmt.Errorf("error unmarshalling poems from the database %v", err)
		}
		if len(items) > 0 {
			item = items[0]
		}
	} else {
		// get poem from database
		poem, err = svc.GetItem(context.TODO(), &dynamodb.GetItemInput{
			TableName: &table,
			Key: map[string]types.AttributeValue{
				"id": &types.AttributeValueMemberS{Value: id},
			},
		})
		if err != nil {
			return nil, fmt.Errorf("error talking to the database: %v", err)
		}
		err = attributevalue.UnmarshalMap(poem.Item, &item)
		if err != nil {
			return nil, fmt.Errorf("error unmarshalling poem from database: %v", err)
		}
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
	var p Poem
	p.ID = fmt.Sprint(art.Id)
	p.Poem = newPoem
	p.AccessionNumber = art.AccessionNumber

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
