package main

import (
	"context"
	"fmt"
	"sync"
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

	addrs, encrypted := ScanBot()
	if len(addrs) == 0 {
		fmt.Println("SwitchBot-Bot not found")
		return
	}
	for i, a := range addrs {
		if encrypted[i] {
			fmt.Printf("%d SwitchBot-Bot (%v) - (encrypted)\n", i+1, a)
		} else {
			fmt.Printf("%d SwitchBot-Bot (%v)\n", i+1, a)
		}
	}
	fmt.Print(" > ")
	var n int
	_, err = fmt.Scanf("%d", &n)
	if err != nil {
		fmt.Println("Invalid input")
		return
	}
	if n < 1 || n > len(addrs) {
		fmt.Println("Invalid input")
		return
	}
	addr := addrs[n-1]
	info, err := InfoBot(addr)
	if err != nil {
		fmt.Printf("Failed to get info: %v\n", err)
		return
	}
	fmt.Printf("SwitchBot-Bot (%v)\n", addr)
	fmt.Println(info)
}

func ScanBot() ([]ble.Addr, []bool) {
	ctx := ble.WithSigHandler(
		context.WithTimeout(context.Background(), 2*time.Second),
	)
	addr := []ble.Addr{}
	encrypted := []bool{}
	var mutex = &sync.Mutex{}

	ble.Scan(ctx,
		false,
		func(a ble.Advertisement) {
			data := a.ServiceData()
			for _, d := range data {
				if !d.UUID.Equal(ble.UUID{0x3d, 0xfd}) {
					continue
				}
				mutex.Lock()
				addr = append(addr, a.Addr())

				// SwitchBot-Botの ServiceData.data は
				// 暗号化なしなら 0x48
				// 暗号化ありなら 0xc8 で始まる
				if d.Data[0] == 0x48 {
					encrypted = append(encrypted, false)
				}
				if d.Data[0] == 0xc8 {
					encrypted = append(encrypted, true)
				}
				mutex.Unlock()
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
	return addr, encrypted
}

type BotInfo struct {
	Battery           int
	Firmware          float64
	Strength          int
	ADC               int
	MotorCalibration  int
	Timer             int
	ActMode           byte
	HoldAndPressTimes int
}

func (b *BotInfo) String() string {
	return fmt.Sprintf("Battery: %d%%, Firmware: %.1f, Strength: %d, ADC: %d, Motor Calibration: %d, Timer: %d, Act Mode: %X, Hold-and-press Times: %d",
		b.Battery, b.Firmware, b.Strength, b.ADC, b.MotorCalibration, b.Timer, b.ActMode, b.HoldAndPressTimes)
}

// SwitchBot-Bot の情報を取得する
func InfoBot(addr ble.Addr) (*BotInfo, error) {
	ctx := ble.WithSigHandler(
		context.WithTimeout(context.Background(), 2*time.Second),
	)
	client, err := ble.Connect(ctx, func(a ble.Advertisement) bool {
		return a.Addr().String() == addr.String()
	})
	if err != nil {
		return nil, err
	}
	if client == nil {
		return nil, fmt.Errorf("client is nil")
	}
	defer client.CancelConnection()

	notifyChar, writeChar, err := getNotifyWriteCharacteristic(client)
	if err != nil {
		return nil, err
	}
	if notifyChar == nil {
		return nil, fmt.Errorf("notify characteristic not found")
	}
	if writeChar == nil {
		return nil, fmt.Errorf("write characteristic not found")
	}
	err = enableNotify(client, notifyChar)
	if err != nil {
		return nil, err
	}

	// 結果をsubscribeで受け取る
	ctx, cancel := context.WithTimeout(
		context.Background(),
		20*time.Second,
	)

	var botinfo *BotInfo
	err = client.Subscribe(
		notifyChar,
		false,
		func(data []byte) {
			cancel()
			if data[0] != 0x01 {
				fmt.Printf("Response Status Error: %v\n", data[0])
				return
			}
			botinfo = &BotInfo{
				Battery:           int(data[1]),
				Firmware:          float64(data[2]) / 10,
				Strength:          int(data[3]),
				ADC:               int(data[4]),
				MotorCalibration:  int(data[5]),
				Timer:             int(data[6]),
				ActMode:           data[7],
				HoldAndPressTimes: int(data[8]),
			}
		},
	)
	if err != nil {
		return nil, err
	}

	// リクエストを送信
	err = client.WriteCharacteristic(
		writeChar,
		[]byte{0x57, 0x02},
		false,
	)
	if err != nil {
		return nil, err
	}

	<-ctx.Done()
	return botinfo, nil
}

func getNotifyWriteCharacteristic(client ble.Client) (*ble.Characteristic, *ble.Characteristic, error) {
	// comminucation service uuid は cba20d00-224d-11e6-9fb8-0002a5d5c51b
	service, err := client.DiscoverServices(
		[]ble.UUID{ble.MustParse("cba20d00-224d-11e6-9fb8-0002a5d5c51b")},
	)
	if err != nil {
		return nil, nil, err
	}
	if len(service) == 0 {
		return nil, nil, fmt.Errorf("service not found")
	}
	// Notify の characteristic UUID は cba20003-224d-11e6-9fb8-0002a5d5c51b
	// Write の characteristic UUID は cba20002-224d-11e6-9fb8-0002a5d5c51b
	chars, err := client.DiscoverCharacteristics(
		[]ble.UUID{ble.MustParse("cba20003-224d-11e6-9fb8-0002a5d5c51b"),
			ble.MustParse("cba20002-224d-11e6-9fb8-0002a5d5c51b")},
		service[0],
	)
	if err != nil {
		return nil, nil, err
	}
	if len(chars) <= 1 {
		return nil, nil, fmt.Errorf("characteristic not found")
	}
	if chars[0].UUID.Equal(ble.MustParse("cba20003-224d-11e6-9fb8-0002a5d5c51b")) {
		return chars[0], chars[1], nil
	}
	return chars[1], chars[0], nil
}

func enableNotify(client ble.Client, notifyChar *ble.Characteristic) error {
	if notifyChar == nil {
		return fmt.Errorf("notify characteristic is nil")
	}
	// descriptor 0x2902 に 0x01 を書き込むことで、notify を有効にする
	descriptor, err := client.DiscoverDescriptors(
		[]ble.UUID{ble.UUID16(0x2902)},
		notifyChar,
	)
	if err != nil {
		return err
	}
	if len(descriptor) == 0 {
		return fmt.Errorf("descriptor not found")
	}
	err = client.WriteDescriptor(
		descriptor[0],
		[]byte{0x01},
	)
	if err != nil {
		return err
	}
	return nil
}
