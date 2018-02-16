package main

import (
	"database/sql"
	"flag"
	"fmt"
	"log"
	"math/big"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/nlopes/slack"

	_ "github.com/lib/pq"
)

var slackBotId string
var slackBotToken string
var slackTipReaction string
var slackTipAmount string
var tokenAddress string
var ethApiEndpoint string
var ethKeyJson string
var ethPassword string

var httpdPort int

var cmdRegex = regexp.MustCompile("^<@[^>]+> ([^<]+) (?:<@)?([^ <>]+)(?:>)?")

func init() {
	slackBotToken = os.Getenv("SLACK_BOT_TOKEN")
	slackTipReaction = os.Getenv("SLACK_TIP_REACTION")
	slackTipAmount = os.Getenv("SLACK_TIP_AMOUNT")
	tokenAddress = os.Getenv("ERC20_TOKEN_ADDRESS")
	ethApiEndpoint = os.Getenv("ETH_API_ENDPOINT")
	ethKeyJson = os.Getenv("ETH_KEY_JSON")
	ethPassword = os.Getenv("ETH_PASSWORD")

	flag.IntVar(&httpdPort, "port", 20020, "port number")
}

func main() {
	flag.Parse()

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "SKRT SKRT")
	})
	go func() {
		log.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", httpdPort), nil))
	}()

	api := slack.New(slackBotToken)
	rtm := api.NewRTM()
	go rtm.ManageConnection()

Loop:
	for {
		select {
		case msg := <-rtm.IncomingEvents:
			switch ev := msg.Data.(type) {
			case *slack.ConnectedEvent:
				slackBotId = ev.Info.User.ID
			case *slack.MessageEvent:
				handleMessage(api, ev)
			case *slack.RTMError:
				fmt.Printf("Error: %s\n", ev.Error())
			case *slack.InvalidAuthEvent:
				fmt.Printf("Invalid credentials")
				break Loop
			// case *slack.ReactionAddedEvent:
			// 	handleReaction(api, ev)
			default:
				// Ignore unknown errors because it's emitted too much time
			}
		}
	}
}

func handleMessage(api *slack.Client, ev *slack.MessageEvent) {
	if !strings.HasPrefix(ev.Text, "<@"+slackBotId+">") {
		return
	}

	// matched := cmdRegex.FindStringSubmatch(ev.Text)
	matched := strings.Split(ev.Text, " ")
	fmt.Println(matched)
	// if len(matched) < 2 {
	// 	fmt.Printf("Leave me alone, Julian")
	// 	return
	// }
	if len(matched) < 2 {
		fmt.Printf("Leave me alone, Julian")
		return
	}
	switch matched[1] {
	case "tip":
		if len(matched) != 4 {
			sendSlackMessage(api, ev.Channel, ":thonk: Usage: tip @user [amount]")
			fmt.Printf("Leave me alone, Julian")
			return
		}
		handleTipCommand(api, ev, matched[2], matched[3])
	case "register":
		if len(matched) != 3 {
			sendSlackMessage(api, ev.Channel, ":thonk: Usage: register [ETH wallet address]")
			fmt.Printf("Leave me alone, Julian")
			return
		}
		handleRegister(api, ev, matched[2])
	case "balance":
		if len(matched) != 2 {
			sendSlackMessage(api, ev.Channel, ":thonk: Usage: balance")
			fmt.Printf("Leave me alone, Julian")
			return
		}
		handleBalanceCommand(api, ev)
	case "withdraw":
		if len(matched) != 2 {
			sendSlackMessage(api, ev.Channel, ":thonk: Usage: withdraw")
			fmt.Printf("Leave me alone, Julian")
			return
		}
		handleWithdrawCommand(api, ev)
	case "help":
		if len(matched) != 2 {
			sendSlackMessage(api, ev.Channel, ":thonk: Usage: help")
			fmt.Printf("Leave me alone, Julian")
			return
		}
		handleHelpCommand(api, ev)
	default:
		fmt.Printf("Unknown command")
	}
}

// func handleReaction(api *slack.Client, ev *slack.ReactionAddedEvent) {
// 	if ev.Reaction != slackTipReaction {
// 		return
// 	}

// 	address := retrieveAddressFor(ev.ItemUser)
// 	if address == "" {
// 		sendSlackMessage(api, ev.ItemUser, `
// :question: Please register your Ethereum address:

// > @tiperc20 register YOUR_ADDRESS
// 		`)
// 	} else {
// 		tx, err := sendTokenTo(address)
// 		if err == nil {
// 			user, _ := api.GetUserInfo(ev.ItemUser)
// 			message := fmt.Sprintf(":+1: You got a token from @%s at %x", user.Profile.RealName, tx.Hash())
// 			sendSlackMessage(api, ev.ItemUser, message)
// 		}
// 	}
// }

func handleHelpCommand(api *slack.Client, ev *slack.MessageEvent) {
	message := ":point_right: :sunglasses: :point_right: I'm a CultureCoin (CULT) tipbot. Try 'tip', 'register', 'balance', or 'withdraw' to interact with me!"
	sendSlackMessage(api, ev.Channel, message)
}

func handleWithdrawCommand(api *slack.Client, ev *slack.MessageEvent) {
	address := retrieveAddressFor(ev.User)
	amount := retrieveBalanceFor(ev.User)

	if amount < 15 {
		sendSlackMessage(api, ev.User, `
:thonk: Must have at least 15 CULT before withdrawing
		`)
	} else if address == "" {
		sendSlackMessage(api, ev.User, `
:point_right: :sunglasses: :point_right: Please register your Ethereum address:

> @tiperc20 register YOUR_ADDRESS
		`)
	} else {
		tx, errr := sendTokenTo(address, amount)
		if errr != nil {
			sendSlackMessage(api, ev.User, ":x: "+errr.Error())
		} else {
			// update withdrawer balance to 0
			db, _ := sql.Open("postgres", os.Getenv("DATABASE_URL"))
			defer db.Close()

			_, err := db.Exec(`
				INSERT INTO balances(slack_user_id, balance) VALUES ($1, $2)
				ON CONFLICT ON CONSTRAINT balances_slack_user_id_key
				DO UPDATE SET balance=$2;
			`, ev.User, 0)

			if err != nil {
				sendSlackMessage(api, ev.Channel, "Looks like I might have lost your CULT. Sorry!")
			}

			// send success message
			// user, _ := api.GetUserInfo(ev.User)
			message := fmt.Sprintf(":point_left: :sunglasses: :point_left: You successfully withdrew %d CULT at %x", amount, tx.Hash())
			sendSlackMessage(api, ev.User, message)
		}
	}
}

func handleBalanceCommand(api *slack.Client, ev *slack.MessageEvent) {
	amount := retrieveBalanceFor(ev.User)
	message := fmt.Sprintf("Your balance is %d CULT", amount)

	db, _ := sql.Open("postgres", os.Getenv("DATABASE_URL"))
	defer db.Close()

	_, err := db.Exec(`
		INSERT INTO balances(slack_user_id, balance) VALUES ($1, $2)
		ON CONFLICT ON CONSTRAINT balances_slack_user_id_key
		DO UPDATE SET balance=$2;
	`, ev.User, amount)

	if err != nil {
		sendSlackMessage(api, ev.User, ":x: "+err.Error())
	}

	sendSlackMessage(api, ev.User, message)
}

func handleTipCommand(api *slack.Client, ev *slack.MessageEvent, userID string, amount string) {
	int_amount, errr := strconv.Atoi(amount)
	if errr != nil {
		log.Printf("Invalid tip amount: %v", errr)
		return
	}

	if int_amount < 1 {
		sendSlackMessage(api, ev.User, `
:thonk: Must send at least 1 CultureCoin (CULT)
		`)
		return
	}

	sender_balance := retrieveBalanceFor(ev.User)

	if sender_balance < int_amount {
		sendSlackMessage(api, ev.User, `
:thonk: Insufficient funds!
		`)
		return
	}

	recipient_balance := retrieveBalanceFor(userID)

	// if recipient_balance == 0 {
	// 	recipient_balance = 0
	// }
	recipient_balance += int_amount

	sender_balance -= int_amount

	// update recipient balnace
	db, _ := sql.Open("postgres", os.Getenv("DATABASE_URL"))
	defer db.Close()

	_, err := db.Exec(`
		INSERT INTO balances(slack_user_id, balance) VALUES ($1, $2)
		ON CONFLICT ON CONSTRAINT balances_slack_user_id_key
		DO UPDATE SET balance=$2;
	`, userID, recipient_balance)

	if err != nil {
		sendSlackMessage(api, ev.Channel, ":thonk: "+err.Error())
	} else {
		user, _ := api.GetUserInfo(ev.User)
		message := fmt.Sprintf(":point_right: :sunglasses: :point_right: %s just sent %s %d CULT!", user.Name, userID, int_amount)
		sendSlackMessage(api, ev.Channel, message)
	}

	// update sender balance
	db, _ = sql.Open("postgres", os.Getenv("DATABASE_URL"))
	defer db.Close()

	_, err = db.Exec(`
		INSERT INTO balances(slack_user_id, balance) VALUES ($1, $2)
		ON CONFLICT ON CONSTRAINT balances_slack_user_id_key
		DO UPDATE SET balance=$2;
	`, ev.User, sender_balance)

	if err != nil {
		sendSlackMessage(api, ev.Channel, ":thonk: "+err.Error())
	} 
	// else {
	// 	sendSlackMessage(api, ev.Channel, ":o: Updated balance")
	// }


	// address := retrieveAddressFor(userID)




// 	if address == "" {
// 		sendSlackMessage(api, userID, `
// :question: Please register your Ethereum address:

// > @tiperc20 register YOUR_ADDRESS
// 		`)
// 	} else {
// 		tx, err := sendTokenTo(address)
// 		if err != nil {
// 			sendSlackMessage(api, ev.Channel, ":x: "+err.Error())
// 		} else {
// 			user, _ := api.GetUserInfo(ev.User)
// 			message := fmt.Sprintf(":+1: You got a token from @%s at %x", user.Profile.RealName, tx.Hash())
// 			sendSlackMessage(api, userID, message)
// 		}
// 	}
}

func handleRegister(api *slack.Client, ev *slack.MessageEvent, address string) {
	userId := ev.User
	stored_address := retrieveAddressFor(userId)

	if address == "" {
		sendSlackMessage(api, ev.User, "Zoop")
		return
	}

	db, _ := sql.Open("postgres", os.Getenv("DATABASE_URL"))
	defer db.Close()

	_, err := db.Exec(`
		INSERT INTO accounts(slack_user_id, ethereum_address) VALUES ($1, $2)
		ON CONFLICT ON CONSTRAINT accounts_slack_user_id_key
		DO UPDATE SET ethereum_address=$2;
	`, userId, address)

	if err != nil {
		sendSlackMessage(api, ev.Channel, ":thonk: "+err.Error())
	} else {
		sendSlackMessage(api, ev.Channel, ":point_right: :sunglasses: :point_right: Registered `"+address+"`")
	}

	// if no stored address, give one time payment of 10 CULT
	if stored_address == "" {
		db, _ := sql.Open("postgres", os.Getenv("DATABASE_URL"))
		defer db.Close()

		_, err := db.Exec(`
			INSERT INTO balances(slack_user_id, balance) VALUES ($1, $2)
			ON CONFLICT ON CONSTRAINT balances_slack_user_id_key
			DO UPDATE SET balance=$2;
		`, ev.User, 10)

		if err != nil {
			sendSlackMessage(api, ev.Channel, ":thonk: "+err.Error())
		} else {
			sendSlackMessage(api, ev.Channel, ":point_left: :sunglasses: :point_left: Enjoy your free 10 CULT!")
		}
	}
}

func sendTokenTo(address string, amount int) (tx *types.Transaction, err error) {
	conn, err := ethclient.Dial(ethApiEndpoint)
	if err != nil {
		log.Printf("Failed to instantiate a Token contract: %v", err)
		return
	}

	token, err := NewToken(common.HexToAddress(tokenAddress), conn)
	if err != nil {
		log.Printf("Failed to instantiate a Token contract: %v", err)
		return
	}

	auth, err := bind.NewTransactor(strings.NewReader(ethKeyJson), ethPassword)
	if err != nil {
		log.Printf("Failed to create authorized transactor: %v", err)
		return
	}

	// amount, err := strconv.ParseInt(slackTipAmount, 10, 64)
	// if err != nil {
	// 	log.Printf("Invalid tip amount: %v", err)
	// 	return
	// }

	tx, err = token.Transfer(auth, common.HexToAddress(address), big.NewInt(int64(amount)))
	if err != nil {
		log.Printf("Failed to request token transfer: %v", err)
		return
	}

	log.Printf("Transfer pending: 0x%x\n", tx.Hash())
	return
}

func sendSlackMessage(api *slack.Client, channel, message string) {
	_, _, err := api.PostMessage(channel, message, slack.PostMessageParameters{})
	if err != nil {
		log.Println(err)
	}
}

func retrieveAddressFor(userID string) (address string) {
	db, _ := sql.Open("postgres", os.Getenv("DATABASE_URL"))
	defer db.Close()

	db.QueryRow(`
		SELECT ethereum_address FROM accounts WHERE slack_user_id = $1 LIMIT 1;
	`, userID).Scan(&address)

	return
}

// TODO retrieve actual balance
func retrieveBalanceFor(userID string) (amount int) {
	db, _ := sql.Open("postgres", os.Getenv("DATABASE_URL"))
	defer db.Close()

	db.QueryRow(`
		SELECT balance FROM balances WHERE slack_user_id = $1 LIMIT 1;
	`, userID).Scan(&amount)

	return
}
