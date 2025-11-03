package trader

import (
	"log"
	"nofx/config"
	"testing"
)

func getConfig() *config.TraderConfig {
	configFile := "../config.json"

	log.Printf("📋 加载配置文件: %s", configFile)
	cfg, err := config.LoadConfig(configFile)
	if err != nil {
		log.Fatalf("❌ 加载配置失败: %v", err)
	}

	for _, traderCfg := range cfg.Traders {
		if traderCfg.Exchange == "gate" {
			return &traderCfg
		}
	}
	return nil
}
func TestGateGetBalance(t *testing.T) {

	conf := getConfig()
	trader, _ := NewGateTrader(conf.GateAPIKey, conf.GateAPISecret, true)
	balance, err := trader.GetBalance()
	if err != nil {
		t.Fatalf("GetBalance failed: %v", err)
	}
	t.Logf("Balance: %+v", balance)

}

func TestGateListPositions(t *testing.T) {
	conf := getConfig()
	trader, _ := NewGateTrader(conf.GateAPIKey, conf.GateAPISecret, true)
	positions, err := trader.GetPositions()
	if err != nil {
		t.Fatalf("positions failed: %v", err)
	}
	t.Logf("positions: %+v", positions)

}

func TestGetMarketPrice(t *testing.T) {
	conf := getConfig()
	trader, _ := NewGateTrader(conf.GateAPIKey, conf.GateAPISecret, true)
	result, err := trader.GetMarketPrice("ETHUSDT")
	if err != nil {
		t.Fatalf("TestGetMarketPrice failed: %v", err)
	}
	t.Logf("TestGetMarketPrice result: %+v", result)
}

func TestOpenLong(t *testing.T) {
	conf := getConfig()
	trader, _ := NewGateTrader(conf.GateAPIKey, conf.GateAPISecret, true)
	result, err := trader.OpenLong("ETH_USDT", 0.1, 5)
	if err != nil {
		t.Fatalf("OpenLong failed: %v", err)
	}
	t.Logf("OpenLong result: %+v", result)
}

func TestCloseLong(t *testing.T) {
	conf := getConfig()
	trader, _ := NewGateTrader(conf.GateAPIKey, conf.GateAPISecret, true)
	result, err := trader.CloseLong("ETH_USDT", 0.1)
	if err != nil {
		t.Fatalf("OpenLong failed: %v", err)
	}
	t.Logf("OpenLong result: %+v", result)
}

func TestOpenShort(t *testing.T) {
	conf := getConfig()
	trader, _ := NewGateTrader(conf.GateAPIKey, conf.GateAPISecret, true)
	result, err := trader.OpenShort("ETH_USDT", 0.1, 5)
	if err != nil {
		t.Fatalf("OpenLong failed: %v", err)
	}
	t.Logf("TestOpenShort result: %+v", result)
}

func TestCloseShort(t *testing.T) {
	conf := getConfig()
	trader, _ := NewGateTrader(conf.GateAPIKey, conf.GateAPISecret, true)
	result, err := trader.CloseShort("ETH_USDT", 0.1)
	if err != nil {
		t.Fatalf("OpenLong failed: %v", err)
	}
	t.Logf("TestCloseShort result: %+v", result)
}

func TestSetTakeProfit(t *testing.T) {
	conf := getConfig()
	trader, _ := NewGateTrader(conf.GateAPIKey, conf.GateAPISecret, true)
	err := trader.SetTakeProfit("ETH_USDT", "SHORT", 0.01, 3700)
	if err != nil {
		t.Fatalf("TestSetStopLoss failed: %v", err)
	}
}
