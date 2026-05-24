package utils

import (
	"fmt"
	"math/rand"
)

var names = []string{
	"Luna", "Max", "Leo", "Milo", "Finn", "Ruby", "Zoe", "Jack", "Lily", "Ollie",
	"Emma", "Chloe", "Sam", "Lucy", "Cooper", "Bailey", "Molly", "Charlie", "Daisy", "Bella",
	"Alice", "Bob", "Eve", "Grace", "Henry", "Ivy", "Kate", "Liam", "Mia", "Noah",
	"Olivia", "Parker", "Quinn", "Rose", "Sophie", "Thomas", "Uma", "Violet", "Will",
	"Xena", "Yuki", "Zara", "Aiden", "Brielle", "Carter", "Delilah", "Elijah", "Freya", "Gavin",
}

func GenerateUserName() string {
	name := names[rand.Intn(len(names))]
	number := rand.Intn(900) + 100
	return fmt.Sprintf("%s%d", name, number)
}
