package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/George-Anagnostou/fridge/fridge"
	cli "github.com/urfave/cli/v2"
)

const serverURL = "http://localhost:8080"

func Run() error {
	app := &cli.App{
		Name:  "fridge-cli",
		Usage: "A CLI to interact with the fridge database",
		Commands: []*cli.Command{
			{
				Name:  "list",
				Usage: "List all items in the fridge",
				Action: func(c *cli.Context) error {
					return listItems()
				},
			},
			{
				Name:  "add",
				Usage: "Add an item to the fridge",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:     "name",
						Aliases:  []string{"n"},
						Usage:    "Name of the item",
						Required: true,
					},
					&cli.IntFlag{
						Name:     "quantity",
						Aliases:  []string{"q"},
						Usage:    "Quantity of the item",
						Required: true,
					},
					&cli.StringFlag{
						Name:     "expires",
						Aliases:  []string{"e"},
						Usage:    "Expiration date of the item (YYYY-MM-DD)",
						Required: false,
					},
				},
				Action: func(c *cli.Context) error {
					date, err := time.Parse(fridge.DateFormat, c.String("expires"))
					if err != nil {
						return err
					}
					item := &fridge.Item{
						Name:           c.String("name"),
						Quantity:       c.Float64("quantity"),
						ExpirationDate: date,
					}
					return addItem(*item)
				},
			},
			{
				Name:  "remove",
				Usage: "Remove an item in the fridge",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:     "id",
						Aliases:  []string{"i"},
						Usage:    "ID of the item",
						Required: true,
					},
				},
				Action: func(c *cli.Context) error {
					return removeItem(c.String("id"))
				},
			},
		},
	}

	return app.Run(os.Args)
}

func listItems() error {
	resp, err := http.Get(serverURL + "/items")
	if err != nil {
		return fmt.Errorf("failed to fetch items: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response: %v", err)
	}

	// var items []map[string]interface{}
	var items []fridge.Item
	if err := json.Unmarshal(body, &items); err != nil {
		return fmt.Errorf("failed to parse JSON: %v", err)
	}

	for _, item := range items {
		fmt.Println(item)
	}

	return nil
}

func addItem(item fridge.Item) error {
	jsonData, err := json.Marshal(item)
	if err != nil {
		return fmt.Errorf("failed to marshal JSON: %v", err)
	}

	resp, err := http.Post(serverURL+"/items", "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to add item: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to add item, status: %s, response: %s", resp.Status, string(body))
	}

	fmt.Println("Item added successfully!")
	return nil
}

func removeItem(id string) error {
	data := url.Values{}
	data.Set("id", id)

	resp, err := http.PostForm(serverURL+"/remove", data)
	if err != nil {
		return fmt.Errorf("failed to remove item: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to remove item, status: %s, response: %s", resp.Status, string(body))
	}

	fmt.Println("Item removed successfully!")
	return nil
}
