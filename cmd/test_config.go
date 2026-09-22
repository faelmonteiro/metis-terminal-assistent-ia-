//go:build ignore

package main

import (
	"fmt"
	"metis-screen/internal/config"
)

func main() {
	cfg := config.Load()
	fmt.Println("Provider:", cfg.Provider)
	fmt.Println("GroqModel:", cfg.GroqModel)
	fmt.Println("ActiveModel for groq:", cfg.GetActiveModelForProvider("groq"))
}
