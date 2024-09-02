package flags

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/George-Anagnostou/fridge/fridge"
)

func Run() error {
	// initialize DB
	db, err := fridge.InitDB()
	if err != nil {
		return err
	}
	defer db.Close()

	// define CLI commands
	addCmd := flag.NewFlagSet("add", flag.ExitOnError)
	listCmd := flag.NewFlagSet("list", flag.ExitOnError)
	removeCmd := flag.NewFlagSet("remove", flag.ExitOnError)

	// define flags for add command
	addName := addCmd.String("name", "", "name of the item")
	addQuantity := addCmd.Float64("qty", 1.0, "quantity of the item")
	addExpiration := addCmd.String("exp", time.Now().AddDate(0, 0, 7).Format(fridge.DateFormat), "Expiration date (yyy-mm-dd)")

	// define flags for remove command
	removeID := removeCmd.Int("id", 0, "ID of the item to remove")

	// define flags for list command
	getID := listCmd.Int("id", 0, "ID of item to get (optional)")

	// check num args
	if len(os.Args) < 2 {
		return fridge.ErrNumArgs
	}

	newFridge := fridge.Fridge{}

	// check action to take from args
	cmd := os.Args[1]

	switch cmd {
	case "add":
		addCmd.Parse(os.Args[2:])
		if *addName == "" {
			fmt.Println("provide a name for the item")
			return fridge.ErrNumArgs
		}
		expirationDate, err := time.Parse(fridge.DateFormat, *addExpiration)
		if err != nil {
			return err
		}

		id, err := newFridge.AddItem(db, *addName, *addQuantity, expirationDate)
		if err != nil {
			return err
		}

		item, err := newFridge.GetItemByID(db, id)
		if err != nil {
			return err
		}

		fmt.Println("Added item :")
		fmt.Println(item)
		return err

	case "list":
		listCmd.Usage = func() {
			fmt.Println("Usage of list: ")
			listCmd.PrintDefaults()
		}
		listCmd.Parse(os.Args[2:])
		if *getID == 0 {
			items, err := newFridge.ListItems(db)
			fmt.Println("Fridge contents:")
			for _, item := range items {
				fmt.Println(item)
			}
			return err

		}
		item, err := newFridge.GetItemByID(db, *getID)
		fmt.Printf("Fridge item %03d\n", *getID)
		fmt.Println(item)
		return err

	case "remove":
		removeCmd.Usage = func() {
			fmt.Println("Usage of remove: ")
			removeCmd.PrintDefaults()
		}
		removeCmd.Parse(os.Args[2:])
		if *removeID == 0 {
			fmt.Println("provide the id of the item to remove")
			return fridge.ErrNumArgs
		}
		fmt.Printf("Removing item %03d\n", *removeID)
		return newFridge.RemoveItem(db, *removeID)

	case "-h", "-help", "--help":
		fmt.Println("Usage of fridge: ")
		fmt.Println("    add     add items to fridge")
		fmt.Println("    list    show all items in fridge")
		fmt.Println("    remove  remove an item from the fridge")
		flag.PrintDefaults()
		return nil

	default:
		fmt.Printf("unknown command: %s\n", cmd)
		PrintUsage()
		return nil
	}
}

func PrintUsage() {
	fmt.Println("Available commands:")
	fmt.Println("    -h -help --help")
	fmt.Println("    add")
	fmt.Println("    list")
	fmt.Println("    remove")
	flag.PrintDefaults()
}
