package poems

import (
	"context"
	"fmt"
	"os"
	"strconv"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/expression"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

type Poem struct {
	ID              string `dynamodbav:"id"`
	AccessionNumber string `dynamodbav:"accession_number"`
	Poem            string `dynamodbav:"poem"`
}

func getClient() (*dynamodb.Client, error) {
	cfg, err := config.LoadDefaultConfig(context.TODO())
	if err != nil {
		return nil, fmt.Errorf("unable to load AWS config: %v", err)
	}

	return dynamodb.NewFromConfig(cfg), nil
}

func PutPoem(poem Poem) error {

	svc, err := getClient()
	if err != nil {
		return fmt.Errorf("could not get dynamodb client %v", err)
	}

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

	svc, err := getClient()
	if err != nil {
		return nil, fmt.Errorf("could not get dynamodb client %v", err)
	}

	var accessionNumber string

	if _, err := strconv.Atoi(id); err != nil {
		accessionNumber = id
	}

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
