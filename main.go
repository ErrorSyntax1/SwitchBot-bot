package main

import (
	"context"
	"fmt"
	"time"

	"github.com/go-ble/ble"
	"github.com/go-ble/ble/linux"
)

// https://github.com/OpenWonderLabs/SwitchBotAPI-BLE/blob/latest/devicetypes/bot.md

func main() {
	device, err := linux.NewDevice()
	if err != nil {
		fmt.Printf("Failed to initialize device: %v\n", err)
		return
	}
	ble.SetDefaultDevice(device)

	err = ScanBot()
	if err != nil {
		fmt.Printf("Scan Finished: %v\n", err)
	}
}

func ScanBot() error {
	ctx := ble.WithSigHandler(
		context.WithTimeout(context.Background(), 2*time.Second),
	)

	err := ble.Scan(ctx,
		false,
		func(a ble.Advertisement) {
			data := a.ServiceData()
			for _, d := range data {
				if !d.UUID.Equal(ble.UUID{0x3d, 0xfd}) {
					continue
				}
				// SwitchBot-Botの ServiceData.data は
				// 暗号化なしなら 0x48
				// 暗号化ありなら 0xc8 で始まる
				if d.Data[0] == 0x48 {
					fmt.Printf("SwitchBot-Bot found: %v\n", a.Addr())
				}
				if d.Data[0] == 0xc8 {
					fmt.Printf("SwitchBot-Bot (encrypted) found: %v\n", a.Addr())
				}
			}
		},
		func(a ble.Advertisement) bool {
			if len(a.ServiceData()) == 0 {
				return false
			}
			// ServiceData.UUID == ble.UUID{0x3d, 0xfd} ならば、SwitchBot製品
			for _, d := range a.ServiceData() {
				if !d.UUID.Equal(ble.UUID{0x3d, 0xfd}) {
					continue
				}
				if d.Data[0] == 0x48 || d.Data[0] == 0xc8 {
					return true
				}
			}
			return false
		},
	)
	return err
}
