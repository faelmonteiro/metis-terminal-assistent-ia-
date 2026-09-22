//go:build ignore

package main
import (
	"fmt"
	"metis-screen/internal/config"
)
func main() {
	cfg := config.Load()
	fmt.Println("Provider:", cfg.Provider)
	fmt.Println("OpenRouterModel:", cfg.OpenRouterModel)
	fmt.Println("ActiveModel:", cfg.GetActiveModelForProvider("openrouter"))
}
