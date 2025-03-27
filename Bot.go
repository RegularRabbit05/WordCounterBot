package main

import (
	"encoding/json"
	"fmt"
	"github.com/bwmarrin/discordgo"
	"log"
	"os"
	"os/signal"
	"sort"
	"strings"
	"sync"
)

type User struct {
	Name  string `json:"name"`
	Count uint64 `json:"count"`
}

type DB struct {
	Users map[string]User `json:"users"`
	Words []string        `json:"words"`
	Emoji string          `json:"emoji"`
	Token string          `json:"token"`
}

type Store struct {
	Data  DB
	Mutex sync.RWMutex
}

func main() {
	var commands = []*discordgo.ApplicationCommand{
		{
			Name:        "count",
			Description: "Check how many times you have said the magic word",
			Options: []*discordgo.ApplicationCommandOption{
				{
					Name:        "user",
					Description: "Specific user to count for",
					Type:        discordgo.ApplicationCommandOptionUser,
					Required:    false,
				},
			},
		},
		{
			Name:        "leaderboard",
			Description: "Check the server leaderboard",
		},
	}

	store := Store{}
	{
		data, err := os.ReadFile("data.json")
		if err != nil {
			log.Println(err)
		} else {
			err = json.Unmarshal(data, &store.Data)
			if err != nil {
				log.Fatalln(err)
			}
			err = os.WriteFile("backup.json", data, 0644)
			if err != nil {
				log.Println(err)
			}
		}
	}
	if store.Data.Users == nil {
		store.Data.Users = make(map[string]User)
	}

	discord, err := discordgo.New("Bot " + store.Data.Token)
	if err != nil {
		log.Fatal(err)
	}
	discord.Identify.Intents |= discordgo.IntentGuildMessages | discordgo.IntentGuildMessageReactions
	err = discord.Open()
	if err != nil {
		log.Fatalln("Error opening connection,", err)
	}
	defer func(discord *discordgo.Session) {
		_ = discord.Close()
	}(discord)

	if discord.UpdateListeningStatus("the chat") != nil {
		log.Fatalln("Unable to update listening status")
	}

	registeredCommands := make([]*discordgo.ApplicationCommand, len(commands))
	for i, v := range commands {
		cmd, err := discord.ApplicationCommandCreate(discord.State.User.ID, "", v)
		if err != nil {
			log.Panicf("Cannot create '%v' command: %v\n", v.Name, err)
		}
		registeredCommands[i] = cmd
	}

	commandHandlers := map[string]func(s *discordgo.Session, i *discordgo.InteractionCreate){
		"count": func(s *discordgo.Session, i *discordgo.InteractionCreate) {
			opt := i.ApplicationCommandData().Options
			if len(opt) == 1 && opt[0].Type == discordgo.ApplicationCommandOptionUser {
				message := "The specified user has never said it!"
				id := opt[0].UserValue(s).ID
				store.Mutex.RLock()
				defer store.Mutex.RUnlock()
				data, ok := store.Data.Users[id]
				if ok {
					message = fmt.Sprintf("%s said the magic word %d times", data.Name, data.Count)
				}
				_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
					Type: discordgo.InteractionResponseChannelMessageWithSource,
					Data: &discordgo.InteractionResponseData{
						Content: message,
					},
				})
				return
			}
			message := "You've never said it!"
			store.Mutex.RLock()
			defer store.Mutex.RUnlock()
			data, ok := store.Data.Users[i.Member.User.ID]
			if ok {
				message = fmt.Sprintf("You've said the magic word %d times", data.Count)
			}
			_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{
					Content: message,
				},
			})
		},
		"leaderboard": func(s *discordgo.Session, i *discordgo.InteractionCreate) {
			message := "Leaderboard empty, what a nice server!"
			store.Mutex.RLock()
			defer store.Mutex.RUnlock()
			if len(store.Data.Users) != 0 {
				var arr []User
				for _, user := range store.Data.Users {
					arr = append(arr, user)
				}
				sort.Slice(arr, func(i, j int) bool { return arr[i].Count > arr[j].Count })
				message = ""
				for _, user := range arr {
					message += fmt.Sprintf("%s: %d\n", user.Name, user.Count)
				}
			}
			_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{
					Content: message,
				},
			})
		},
	}

	discord.AddHandler(func(s *discordgo.Session, i *discordgo.InteractionCreate) {
		if h, ok := commandHandlers[i.ApplicationCommandData().Name]; ok {
			h(s, i)
		}
	})
	discord.AddHandler(func(s *discordgo.Session, i *discordgo.MessageCreate) {
		if i.Author.Bot || i.Author.System {
			return
		}
		message := strings.ToLower(i.Message.Content)
		store.Mutex.Lock()
		defer store.Mutex.Unlock()
		for _, word := range store.Data.Words {
			if strings.Contains(message, strings.ToLower(word)) {
				usr, ok := store.Data.Users[i.Author.ID]
				if !ok {
					usr = User{}
				}
				usr.Count++
				usr.Name = i.Author.Username
				store.Data.Users[i.Author.ID] = usr
				err = s.MessageReactionAdd(i.ChannelID, i.ID, store.Data.Emoji)
				if err != nil {
					log.Println(err)
				}
				rankingsJson, err := json.Marshal(store.Data)
				if err != nil {
					log.Println(err)
					break
				}
				err = os.WriteFile("data.json", rankingsJson, 0644)
				if err != nil {
					log.Println(err)
				}
				break
			}
		}
	})
	log.Println("The bot is now running. Press CTRL-C to exit.")

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt)
	<-stop
	log.Println("Shutting down...")
	store.Mutex.Lock()
	store.Mutex.Unlock()
}
