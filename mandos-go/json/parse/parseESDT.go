package mandosjsonparse

import (
	"errors"
	"fmt"

	mj "github.com/ElrondNetwork/arwen-wasm-vm/mandos-go/json/model"
	oj "github.com/ElrondNetwork/arwen-wasm-vm/mandos-go/orderedjson"
)

func (p *Parser) processAppendESDTData(tokenName []byte, esdtDataRaw oj.OJsonObject, output []*mj.ESDTData) ([]*mj.ESDTData, error) {
	var err error

	switch data := esdtDataRaw.(type) {
	case *oj.OJsonString:
		// simple string representing balance "400,000,000,000"
		esdtData := mj.ESDTData{}
		esdtData.TokenIdentifier = mj.NewJSONBytesFromString(tokenName, string(tokenName))
		esdtData.Value, err = p.processBigInt(esdtDataRaw, bigIntUnsignedBytes)
		if err != nil {
			return output, fmt.Errorf("invalid ESDT balance: %w", err)
		}

		output = append(output, &esdtData)
		return output, nil
	case *oj.OJsonMap:
		esdtData, err := p.processESDTDataMap(tokenName, data)
		if err != nil {
			return output, err
		}
		output = append(output, esdtData)
		return output, nil
	case *oj.OJsonList:
		for _, item := range data.AsList() {
			itemAsMap, isMap := item.(*oj.OJsonMap)
			if !isMap {
				return nil, errors.New("JSON map expected in ESDT list")
			}
			esdtData, err := p.processESDTDataMap(tokenName, itemAsMap)
			if err != nil {
				return output, err
			}
			output = append(output, esdtData)
		}
		return output, nil
	default:
		return output, errors.New("invalid JSON object for ESDT")
	}
}

func (p *Parser) processTxESDT(esdtRaw oj.OJsonObject) (*mj.ESDTData, error) {
	esdtDataMap, isMap := esdtRaw.(*oj.OJsonMap)
	if !isMap {
		return nil, errors.New("unmarshalled account object is not a map")
	}
	return p.processESDTDataMap([]byte{}, esdtDataMap)
}

// map containing other fields too, e.g.:
// {
// 	"balance": "400,000,000,000",
// 	"frozen": "true"
// }
func (p *Parser) processESDTDataMap(tokenNameKey []byte, esdtDataMap *oj.OJsonMap) (*mj.ESDTData, error) {
	esdtData := mj.ESDTData{
		TokenIdentifier: mj.NewJSONBytesFromString(tokenNameKey, ""),
	}
	var err error

	for _, kvp := range esdtDataMap.OrderedKV {
		switch kvp.Key {
		case "tokenIdentifier":
			esdtData.TokenIdentifier, err = p.processStringAsByteArray(kvp.Value)
			if err != nil {
				return nil, fmt.Errorf("invalid ESDT token name: %w", err)
			}
		case "nonce":
			esdtData.Nonce, err = p.processUint64(kvp.Value)
			if err != nil {
				return nil, errors.New("invalid account nonce")
			}
		case "value":
			esdtData.Value, err = p.processBigInt(kvp.Value, bigIntUnsignedBytes)
			if err != nil {
				return nil, fmt.Errorf("invalid ESDT balance: %w", err)
			}
		case "frozen":
			esdtData.Frozen, err = p.processUint64(kvp.Value)
			if err != nil {
				return nil, fmt.Errorf("invalid ESDT frozen flag: %w", err)
			}
		default:
			return nil, fmt.Errorf("unknown ESDT data field: %s", kvp.Key)
		}
	}

	return &esdtData, nil
}

func (p *Parser) processCheckESDTData(esdtDataRaw oj.OJsonObject) (*mj.CheckESDTData, error) {
	esdtData := mj.CheckESDTData{}
	var err error

	if _, isStr := esdtDataRaw.(*oj.OJsonString); isStr {
		// simple string representing balance "400,000,000,000"
		esdtData.Balance, err = p.processCheckBigInt(esdtDataRaw, bigIntUnsignedBytes)
		if err != nil {
			return nil, fmt.Errorf("invalid ESDT balance: %w", err)
		}
		return &esdtData, nil
	}

	// map containing other fields too, e.g.:
	// {
	// 	"balance": "400,000,000,000",
	// 	"frozen": "true"
	// }
	esdtDataMap, isMap := esdtDataRaw.(*oj.OJsonMap)
	if !isMap {
		return nil, errors.New("account ESDT data should be either JSON string or map")
	}

	for _, kvp := range esdtDataMap.OrderedKV {
		switch kvp.Key {
		case "balance":
			esdtData.Balance, err = p.processCheckBigInt(kvp.Value, bigIntUnsignedBytes)
			if err != nil {
				return nil, fmt.Errorf("invalid ESDT balance: %w", err)
			}
		case "frozen":
			esdtData.Frozen, err = p.processCheckUint64(kvp.Value)
			if err != nil {
				return nil, fmt.Errorf("invalid ESDT frozen flag: %w", err)
			}
		default:
			return nil, fmt.Errorf("unknown ESDT data field: %s", kvp.Key)
		}
	}

	return &esdtData, nil
}
