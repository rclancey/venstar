package main

import (
	"flag"
	"fmt"
	"log"
	"math"
	"strings"
	"time"

	"github.com/rclancey/venstar"
)

type argsType struct {
	zone     string
	heatTemp float64
	coolTemp float64
}

func parseArgs() argsType {
	var args argsType
	flag.StringVar(&args.zone, "zone", "", "thermostat to control")
	flag.Float64Var(&args.heatTemp, "heat", math.NaN(), "threshold temp for heating")
	flag.Float64Var(&args.coolTemp, "cool", math.NaN(), "threshold temp for cooling")
	flag.Parse()
	return args
}

func main() {
	args := parseArgs()
	ch, err := venstar.Discover(2*time.Second)
	if err != nil {
		log.Fatal(err)
	}
	if args.zone == "" {
		fmt.Println("Known thermostat zones:")
		for {
			dev, ok := <-ch
			if !ok || dev == nil {
				break
			}
			fmt.Println("  ", dev.Name)
		}
	} else {
		for {
			dev, ok := <-ch
			if !ok || dev == nil {
				break
			}
			if strings.ToLower(dev.Name) != strings.ToLower(args.zone) {
				continue
			}
			if !math.IsNaN(args.heatTemp) {
				if !math.IsNaN(args.coolTemp) {
					err = dev.SetHeatCoolTemps(args.heatTemp, args.coolTemp)
				} else {
					err = dev.SetHeatTemp(args.heatTemp)
				}
			} else if !math.IsNaN(args.coolTemp) {
				err = dev.SetCoolTemp(args.coolTemp)
			}
			if err != nil {
				log.Fatal(err)
			}
			info, err := dev.Info()
			if err != nil {
				log.Fatal(err)
			}
			fmt.Println("Current Temp:", info.SpaceTemp)
			fmt.Println("Heat Temp:", info.HeatTemp)
			fmt.Println("Cool Temp:", info.CoolTemp)
			break
		}
	}
}
