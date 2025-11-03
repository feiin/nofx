package trader

import (
	"context"
	"fmt"
	"log"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/adshao/go-binance/v2/futures"
	"github.com/gateio/gateapi-go/v7"
)

type GateConfig struct {
	ApiKey     string
	ApiSecret  string
	BaseUrl    string
	UseTestNet bool
}

func NewGateConfig(apiKey string, apiSecret string, useTestNet bool) *GateConfig {
	config := &GateConfig{
		ApiKey:     apiKey,
		ApiSecret:  apiSecret,
		UseTestNet: useTestNet,
		BaseUrl:    "https://api.gateio.ws/api/v4",
	}
	if useTestNet {
		config.BaseUrl = "https://api-testnet.gateapi.io/api/v4"
		// config.BaseUrl = "https://fx-api-testnet.gateio.ws/api/v4"
	}
	log.Printf("config %+v", config)

	return config
}

type GateTrader struct {
	client *gateapi.APIClient
	config *GateConfig

	// 余额缓存
	cachedBalance     map[string]interface{}
	balanceCacheTime  time.Time
	balanceCacheMutex sync.RWMutex

	// 持仓缓存
	cachedPositions     []map[string]interface{}
	positionsCacheTime  time.Time
	positionsCacheMutex sync.RWMutex

	// 缓存有效期（15秒）
	cacheDuration time.Duration
}

func NewGateTrader(apiKey, secretKey string, useTestNet bool) (*GateTrader, error) {
	config := NewGateConfig(apiKey, secretKey, useTestNet)

	clientConfig := gateapi.NewConfiguration()
	clientConfig.BasePath = config.BaseUrl
	client := gateapi.NewAPIClient(clientConfig)
	return &GateTrader{
		client:        client,
		config:        config,
		cacheDuration: 15 * time.Second, // 15秒缓存
	}, nil
}

func (t *GateTrader) getClientCtx() context.Context {
	ctx := context.WithValue(context.Background(),
		gateapi.ContextGateAPIV4,
		gateapi.GateAPIV4{
			Key:    t.config.ApiKey,
			Secret: t.config.ApiSecret,
		})
	return ctx
}

// GetMarketPrice 获取市场价格
func (t *GateTrader) GetMarketPrice(symbol string) (float64, error) {
	symbol = formatSymbolToContract(symbol)

	settle := "usdt"
	ticker, _, err := t.client.FuturesApi.GetFuturesContract(t.getClientCtx(), settle, symbol)
	if err != nil {
		return 0, fmt.Errorf("获取行情失败: %w", err)
	}

	price, err := strconv.ParseFloat(ticker.LastPrice, 64)
	if err != nil {
		return 0, fmt.Errorf("解析价格失败: %w", err)
	}

	log.Printf("📈 %s 当前市价: %.2f", symbol, price)
	return price, nil
}

// GetBalance 获取账户余额（带缓存）
func (t *GateTrader) GetBalance() (map[string]interface{}, error) {
	// 先检查缓存是否有效
	t.balanceCacheMutex.RLock()
	if t.cachedBalance != nil && time.Since(t.balanceCacheTime) < t.cacheDuration {
		cacheAge := time.Since(t.balanceCacheTime)
		t.balanceCacheMutex.RUnlock()
		log.Printf("✓ 使用缓存的账户余额（缓存时间: %.1f秒前）", cacheAge.Seconds())
		return t.cachedBalance, nil
	}
	t.balanceCacheMutex.RUnlock()

	// 缓存过期或不存在，调用API
	log.Printf("🔄 缓存过期，正在调用GateAPI获取账户余额...")
	account, _, err := t.client.FuturesApi.ListFuturesAccounts(t.getClientCtx(), "usdt")
	if err != nil {
		log.Printf("❌ GateAPI调用失败: %v", err)
		return nil, fmt.Errorf("获取账户信息失败: %w", err)
	}

	totalWalletBalance, _ := strconv.ParseFloat(account.Total, 64)
	totalUnrealizedProfit, _ := strconv.ParseFloat(account.UnrealisedPnl, 64)
	availableBalance := totalWalletBalance - totalUnrealizedProfit
	result := make(map[string]interface{})
	result["totalWalletBalance"] = totalWalletBalance
	result["totalUnrealizedProfit"] = totalUnrealizedProfit
	result["availableBalance"] = availableBalance
	log.Printf("✓ GateAPI返回: 总余额=%.2f, 可用=%.2f, 未实现盈亏=%.2f", totalWalletBalance, availableBalance, totalUnrealizedProfit)

	return result, nil
}

// GetPositions 获取所有持仓（带缓存）
func (t *GateTrader) GetPositions() ([]map[string]interface{}, error) {
	// 先检查缓存是否有效
	t.positionsCacheMutex.RLock()
	if t.cachedPositions != nil && time.Since(t.positionsCacheTime) < t.cacheDuration {
		cacheAge := time.Since(t.positionsCacheTime)
		t.positionsCacheMutex.RUnlock()
		log.Printf("✓ 使用缓存的持仓信息（缓存时间: %.1f秒前）", cacheAge.Seconds())
		return t.cachedPositions, nil
	}
	t.positionsCacheMutex.RUnlock()

	// 缓存过期或不存在，调用API
	settle := "usdt"
	log.Printf("🔄 缓存过期，正在调用Gate API获取持仓信息...")
	positions, _, err := t.client.FuturesApi.ListPositions(t.getClientCtx(), settle, nil)
	if err != nil {
		return nil, fmt.Errorf("获取持仓失败: %w", err)
	}

	var result []map[string]interface{}
	for _, pos := range positions {
		posAmt := pos.Size
		if posAmt == 0 {
			continue // 跳过无持仓的
		}

		posMap := make(map[string]interface{})
		posMap["symbol"] = pos.Contract
		posMap["positionAmt"] = float64(posAmt)
		posMap["entryPrice"], _ = strconv.ParseFloat(pos.EntryPrice, 64)
		posMap["markPrice"], _ = strconv.ParseFloat(pos.MarkPrice, 64)
		posMap["unRealizedProfit"], _ = strconv.ParseFloat(pos.UnrealisedPnl, 64)
		posMap["leverage"], _ = strconv.ParseFloat(pos.Leverage, 64)
		posMap["liquidationPrice"], _ = strconv.ParseFloat(pos.LiqPrice, 64)

		// 判断方向
		if posAmt > 0 {
			posMap["side"] = "long"
		} else {
			posMap["side"] = "short"
		}

		result = append(result, posMap)
	}

	// 更新缓存
	t.positionsCacheMutex.Lock()
	t.cachedPositions = result
	t.positionsCacheTime = time.Now()
	t.positionsCacheMutex.Unlock()

	return result, nil
}

// SetLeverage 设置杠杆（智能判断+冷却期）
func (t *GateTrader) SetLeverage(symbol string, leverage int) error {
	symbol = formatSymbolToContract(symbol)

	// 先尝试获取当前杠杆（从持仓信息）
	currentLeverage := 0
	positions, err := t.GetPositions()
	if err == nil {
		for _, pos := range positions {
			if pos["symbol"] == symbol {
				if lev, ok := pos["leverage"].(float64); ok {
					currentLeverage = int(lev)
					break
				}
			}
		}
	}

	// 如果当前杠杆已经是目标杠杆，跳过
	if currentLeverage == leverage && currentLeverage > 0 {
		log.Printf("  ✓ %s 杠杆已是 %dx，无需切换", symbol, leverage)
		return nil
	}

	// 切换杠杆
	settle := "usdt"
	strLeverage := strconv.Itoa(leverage)
	log.Printf("🔄 切换 %s 杠杆: %dx -> %dx", symbol, currentLeverage, leverage)
	_, _, err = t.client.FuturesApi.UpdatePositionLeverage(t.getClientCtx(), settle, symbol, strLeverage, nil)

	if err != nil {
		// 如果错误信息包含"No need to change"，说明杠杆已经是目标值
		if contains(err.Error(), "No need to change") {
			log.Printf("  ✓ %s 杠杆已是 %dx", symbol, leverage)
			return nil
		}
		return fmt.Errorf("设置杠杆失败: %w", err)
	}

	log.Printf("  ✓ %s 杠杆已切换为 %dx", symbol, leverage)

	// 切换杠杆后等待5秒（避免冷却期错误）
	log.Printf("  ⏱ 等待5秒冷却期...")
	time.Sleep(5 * time.Second)

	return nil
}

// CancelAllOrders 取消该币种的所有挂单
func (t *GateTrader) CancelAllOrders(symbol string) error {
	settle := "usdt"
	symbol = formatSymbolToContract(symbol)

	_, _, err := t.client.FuturesApi.CancelFuturesOrders(t.getClientCtx(), settle, symbol, nil)

	if err != nil {
		return fmt.Errorf("取消挂单失败: %w", err)
	}

	log.Printf("  ✓ 已取消 %s 的所有挂单", symbol)
	return nil
}

// GetSymbolPrecision 获取合约交易对的精度信息（价格精度、小数单位、最小下单量、每张合约乘数）
func (t *GateTrader) GetSymbolPrecision(symbol string) (pricePrecision int, sizeMin float64, quanto float64, err error) {
	symbol = formatSymbolToContract(symbol)

	contracts, _, err := t.client.FuturesApi.ListFuturesContracts(t.getClientCtx(), "usdt", nil)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("获取合约交易规则失败: %w", err)
	}

	for _, c := range contracts {
		if strings.EqualFold(c.Name, symbol) {
			pricePrecision = getPrecisionFromRound(c.OrderPriceRound)
			sizeMin = float64(c.OrderSizeMin)

			quanto, _ = strconv.ParseFloat(c.QuantoMultiplier, 64)
			if quanto == 0 {
				quanto = 1 // 安全兜底
			}

			log.Printf("✅ %s 精度信息: 价格精度=%d, 最小下单量=%.0f张, QuantoMultiplier=%f",
				symbol, pricePrecision, sizeMin, quanto)
			return pricePrecision, sizeMin, quanto, nil
		}
	}

	log.Printf("⚠ 未找到 %s 的精度信息，使用默认精度(价格精度3, 最小下单量1, 乘数1)", symbol)
	return 3, 1, 1, nil
}

// getPrecisionFromRound 根据字符串 "0.001" 推算小数位数
func getPrecisionFromRound(round string) int {
	if !strings.Contains(round, ".") {
		return 0
	}
	decimals := strings.TrimRight(strings.Split(round, ".")[1], "0")
	return len(decimals)
}

// FormatQuantity 仅用于日志输出格式化
func (t *GateTrader) FormatQuantity(symbol string, quantity float64) (string, error) {
	precision, _, _, err := t.GetSymbolPrecision(symbol)
	if err != nil {
		precision = 3 // fallback
	}
	format := fmt.Sprintf("%%.%df", precision)
	return fmt.Sprintf(format, quantity), nil
}

// quantityToContractSize 将标的币数量换算为合约张数（向下取整）
func (t *GateTrader) quantityToContractSize(symbol string, quantity float64) (int64, error) {
	_, sizeMin, quanto, err := t.GetSymbolPrecision(symbol)
	if err != nil {
		return 0, err
	}

	if quantity <= 0 {
		return 0, fmt.Errorf("数量必须大于0: %.8f", quantity)
	}

	sizeFloat := quantity / quanto
	sizeInt := int64(math.Floor(sizeFloat)) // 向下取整更安全
	if sizeInt < int64(sizeMin) {
		return 0, fmt.Errorf("下单量 %.8f 太小, 对应 %.4f 张, 小于最小张数 %.0f", quantity, sizeFloat, sizeMin)
	}

	return sizeInt, nil
}

func formatSymbolToContract(symbol string) string {
	// BTCUSDT -> BTC_USDT
	if strings.Contains(symbol, "_") {
		return symbol
	}
	return strings.ReplaceAll(strings.ToUpper(symbol), "USDT", "_USDT")
}

// SetMarginMode 设置仓位模式
func (t *GateTrader) SetMarginMode(symbol string, isCrossMargin bool) error {
	var marginType futures.MarginType
	if isCrossMargin {
		marginType = futures.MarginTypeCrossed
	} else {
		marginType = futures.MarginTypeIsolated
	}
	settle := "usdt"
	_, _, err := t.client.FuturesApi.UpdateDualCompPositionCrossMode(t.getClientCtx(), settle, gateapi.InlineObject{
		Contract: symbol,
		Mode:     string(marginType),
	})
	// 尝试设置仓位模式

	marginModeStr := "全仓"
	if !isCrossMargin {
		marginModeStr = "逐仓"
	}

	if err != nil {
		// 如果错误信息包含"No need to change"，说明仓位模式已经是目标值
		if contains(err.Error(), "No need to change margin type") {
			log.Printf("  ✓ %s 仓位模式已是 %s", symbol, marginModeStr)
			return nil
		}
		// 如果有持仓，无法更改仓位模式，但不影响交易
		if contains(err.Error(), "Margin type cannot be changed if there exists position") {
			log.Printf("  ⚠️ %s 有持仓，无法更改仓位模式，继续使用当前模式", symbol)
			return nil
		}
		log.Printf("  ⚠️ 设置仓位模式失败: %v", err)
		// 不返回错误，让交易继续
		return nil
	}

	log.Printf("  ✓ %s 仓位模式已设置为 %s", symbol, marginModeStr)
	return nil
}

// OpenLong 开多仓（市价单）
func (t *GateTrader) OpenLong(symbol string, quantity float64, leverage int) (map[string]interface{}, error) {
	settle := "usdt"

	symbol = formatSymbolToContract(symbol)
	// 1️⃣ 取消旧委托
	if err := t.CancelAllOrders(symbol); err != nil {
		log.Printf("⚠️ 取消旧委托单失败（可能没有未完成订单）: %v", err)
	}

	// 2️⃣ 设置杠杆
	if err := t.SetLeverage(symbol, leverage); err != nil {
		return nil, fmt.Errorf("设置杠杆失败: %w", err)
	}

	// 3️⃣ 设置逐仓模式
	_, _, err := t.client.FuturesApi.UpdateDualCompPositionCrossMode(t.getClientCtx(), settle, gateapi.InlineObject{
		Contract: symbol,
		Mode:     "ISOLATED",
	})
	if err != nil {
		log.Printf("⚠️ 设置逐仓模式失败: %v（可能已是逐仓模式）", err)
	}

	// 4️⃣ 换算数量为合约张数
	sizeInt, err := t.quantityToContractSize(symbol, quantity)
	if err != nil {
		return nil, fmt.Errorf("换算下单张数失败: %w", err)
	}

	// 5️⃣ 创建市价多单
	order := gateapi.FuturesOrder{
		Contract: symbol,
		Size:     sizeInt, // 正数 = 开多
		Price:    "0",     // 市价单
		Tif:      "ioc",   // 立即成交或取消
		Text:     "t-open_long",
	}

	resp, _, err := t.client.FuturesApi.CreateFuturesOrder(t.getClientCtx(), settle, order, nil)
	if err != nil {
		return nil, fmt.Errorf("开多仓失败: %w", err)
	}

	log.Printf("✅ 开多成功: %s 数量(%.6f币)=%d张, 杠杆=%dx, 订单ID=%v",
		symbol, quantity, sizeInt, leverage, resp.Id)

	result := map[string]interface{}{
		"orderId": resp.Id,
		"symbol":  resp.Contract,
		"status":  resp.Status,
		"price":   resp.Price,
		"size":    resp.Size,
	}
	return result, nil
}

// CloseLong 平多仓（市价平仓）
func (t *GateTrader) CloseLong(symbol string, quantity float64) (map[string]interface{}, error) {
	settle := "usdt"
	symbol = formatSymbolToContract(symbol)

	// 1️⃣ 如果用户没传数量，则自动获取当前持仓数量
	if quantity == 0 {
		positions, _, err := t.client.FuturesApi.ListPositions(t.getClientCtx(), settle, nil)
		if err != nil {
			return nil, fmt.Errorf("获取持仓失败: %w", err)
		}

		found := false
		for _, pos := range positions {
			// 多仓：Size > 0
			if strings.EqualFold(pos.Contract, symbol) && pos.Size > 0 {
				// Gate返回的是张数
				quantity = float64(pos.Size)
				found = true
				break
			}
		}

		if !found || quantity == 0 {
			return nil, fmt.Errorf("没有找到 %s 的多仓可平", symbol)
		}
		log.Printf("📊 自动检测到 %s 多仓数量: %.0f 张", symbol, quantity)
	}

	// 2️⃣ 获取合约精度信息（新版 GetSymbolPrecision）
	pricePrecision, sizeMin, quanto, err := t.GetSymbolPrecision(symbol)
	if err != nil {
		log.Printf("⚠️ 获取精度信息失败，使用默认参数")
		sizeMin = 1
		pricePrecision = 3
		quanto = 1
	}

	// 3️⃣ 将传入的币数量转换成张数（Gate Futures 下单单位是“张”）
	sizeFloat := quantity / quanto
	sizeInt := int64(math.Round(sizeFloat))

	if float64(sizeInt) < sizeMin {
		return nil, fmt.Errorf("平仓数量 %.6f 转换后不足最小下单量 %.0f张 (每张=%f币)", quantity, sizeMin, quanto)
	}
	if sizeInt <= 0 {
		return nil, fmt.Errorf("无效的平仓数量: %.6f (计算后张数=%d)", quantity, sizeInt)
	}

	// 4️⃣ 构建市价平多单（负数代表平多）
	order := gateapi.FuturesOrder{
		Contract: symbol,
		Size:     -sizeInt,       // ❗负数代表平多仓（卖出）
		Price:    "0",            // 市价单
		Tif:      "ioc",          // 立即成交或取消
		Text:     "t-close_long", // Gate要求text以`t-`开头
	}

	resp, _, err := t.client.FuturesApi.CreateFuturesOrder(t.getClientCtx(), settle, order, nil)
	if err != nil {
		return nil, fmt.Errorf("平多仓失败: %w", err)
	}

	// 5️⃣ 输出执行结果
	log.Printf("✅ 平多仓成功: %s 数量(%.6f币)=%.0f张", symbol, quantity, float64(sizeInt))
	log.Printf("📄 订单ID: %d | 状态: %s | 价格精度: %d | 乘数: %f",
		resp.Id, resp.Status, pricePrecision, quanto)

	// 6️⃣ 平仓后取消该币种的挂单（止盈止损单）
	if err := t.CancelAllOrders(symbol); err != nil {
		log.Printf("⚠️ 取消挂单失败（可能无挂单）: %v", err)
	}

	// 7️⃣ 封装结果返回
	result := map[string]interface{}{
		"orderId": resp.Id,
		"symbol":  resp.Contract,
		"status":  resp.Status,
	}

	return result, nil
}

// OpenShort 开空仓
func (t *GateTrader) OpenShort(symbol string, quantity float64, leverage int) (map[string]interface{}, error) {
	symbol = formatSymbolToContract(symbol)

	// 先取消该币种的所有委托单（清理旧的止损止盈单）
	if err := t.CancelAllOrders(symbol); err != nil {
		log.Printf("  ⚠ 取消旧委托单失败（可能没有委托单）: %v", err)
	}

	// 设置杠杆
	if err := t.SetLeverage(symbol, leverage); err != nil {
		return nil, err
	}

	// 3️⃣ 设置逐仓模式
	settle := "usdt"
	_, _, err := t.client.FuturesApi.UpdateDualCompPositionCrossMode(t.getClientCtx(), settle, gateapi.InlineObject{
		Contract: symbol,
		Mode:     "ISOLATED",
	})
	if err != nil {
		log.Printf("⚠️ 设置逐仓模式失败: %v（可能已是逐仓模式）", err)
	}

	// 换算数量为合约张数
	sizeInt, err := t.quantityToContractSize(symbol, quantity)
	if err != nil {
		return nil, fmt.Errorf("换算下单张数失败: %w", err)
	}

	// 创建市价空单
	order := gateapi.FuturesOrder{
		Contract: symbol,
		Size:     -sizeInt, // 负数 = 开空
		Price:    "0",      // 市价单
		Tif:      "ioc",    // 立即成交或取消
		Text:     "t-open_short",
	}

	respOrder, _, err := t.client.FuturesApi.CreateFuturesOrder(t.getClientCtx(), settle, order, nil)
	if err != nil {
		return nil, fmt.Errorf("开空仓失败: %w", err)
	}

	log.Printf("✓ 开空仓成功: %s 数量: %d", symbol, sizeInt)
	log.Printf("  订单ID: %d", respOrder.Id)

	result := make(map[string]interface{})
	result["orderId"] = respOrder.Id
	result["symbol"] = symbol
	result["status"] = respOrder.Status
	return result, nil
}

// CloseShort 平空仓
func (t *GateTrader) CloseShort(symbol string, quantity float64) (map[string]interface{}, error) {
	settle := "usdt"

	symbol = formatSymbolToContract(symbol)

	// 1️⃣ 如果用户没传数量，则自动获取当前持仓数量
	if quantity == 0 {
		positions, _, err := t.client.FuturesApi.ListPositions(t.getClientCtx(), settle, nil)
		if err != nil {
			return nil, fmt.Errorf("获取持仓失败: %w", err)
		}

		found := false
		for _, pos := range positions {
			// 空仓：Size < 0
			if strings.EqualFold(pos.Contract, symbol) && pos.Size < 0 {
				// Gate返回的是张数
				quantity = float64(-pos.Size) // 取正数
				found = true
				break
			}
		}

		if !found || quantity == 0 {
			return nil, fmt.Errorf("没有找到 %s 的空仓可平", symbol)
		}
		log.Printf("📊 自动检测到 %s 空仓数量: %.0f 张", symbol, quantity)
	}

	// 2️⃣ 获取合约精度信息（新版 GetSymbolPrecision）
	pricePrecision, sizeMin, quanto, err := t.GetSymbolPrecision(symbol)
	if err != nil {
		log.Printf("⚠️ 获取精度信息失败，使用默认参数")
		sizeMin = 1
		pricePrecision = 3
		quanto = 1
	}

	// 3️⃣ 将传入的币数量转换成张数（Gate Futures 下单单位是“张”）
	sizeFloat := quantity / quanto
	sizeInt := int64(math.Round(sizeFloat))

	if float64(sizeInt) < sizeMin {
		return nil, fmt.Errorf("平仓数量 %.6f 转换后不足最小下单量 %.0f张 (每张=%f币)", quantity, sizeMin, quanto)
	}
	if sizeInt <= 0 {
		return nil, fmt.Errorf("无效的平仓数量: %.6f (计算后张数=%d)", quantity, sizeInt)
	}

	// 4️⃣ 构建市价平空单（正数代表平空）
	order := gateapi.FuturesOrder{
		Contract: symbol,
		Size:     sizeInt,         // ❗正数代表平空仓（买入）
		Price:    "0",             // 市价单
		Tif:      "ioc",           // 立即成交或取消
		Text:     "t-close_short", // Gate要求text以`t-`开头
	}

	resp, _, err := t.client.FuturesApi.CreateFuturesOrder(t.getClientCtx(), settle, order, nil)
	if err != nil {
		return nil, fmt.Errorf("平空仓失败: %w", err)
	}

	// 5️⃣ 输出执行结果
	log.Printf("✅ 平空仓成功: %s 数量(%.6f币)=%.0f张", symbol, quantity, float64(sizeInt))
	log.Printf("📄 订单ID: %d | 状态: %s | 价格精度: %d | 乘数: %f", resp.Id, resp.Status, pricePrecision, quanto)

	result := make(map[string]interface{})
	result["orderId"] = resp.Id
	result["symbol"] = symbol
	result["status"] = resp.Status
	return result, nil
}

// SetStopLoss 设置止损单（基于 price-triggered order）
func (t *GateTrader) SetStopLoss(symbol string, positionSide string, quantity, stopPrice float64) error {
	settle := "usdt"
	symbol = formatSymbolToContract(symbol)

	// 参数校验
	side := strings.ToLower(strings.TrimSpace(positionSide))
	if side != "long" && side != "short" {
		return fmt.Errorf("positionSide 必须是 'long' 或 'short'")
	}
	if quantity <= 0 {
		return fmt.Errorf("quantity 必须大于 0")
	}
	if stopPrice <= 0 {
		return fmt.Errorf("stopPrice 必须大于 0")
	}

	// 根据方向确定触发规则与下单方向
	var rule int32
	// var orderSize int64
	if side == "long" {
		// 当前价 ≤ stopPrice
		rule = 2
	} else {
		// 当前价 ≥ stopPrice
		rule = 1
	}

	// 构建触发条件
	trigger := gateapi.FuturesPriceTrigger{
		Price:     fmt.Sprintf("%f", stopPrice),
		Rule:      rule, // 1: <=, 2: >=
		PriceType: 1,    // 1: mark_price（标记价触发）
		// Expiration:   86400, // 1天有效
		StrategyType: 0, // 默认

	}

	// 构建触发后的下单参数
	initial := gateapi.FuturesInitialOrder{
		Contract: symbol,
		Size:     0,
		Price:    "0",   // 市价单
		Tif:      "ioc", // 立即成交
		Close:    true,  // 平仓
		Text:     fmt.Sprintf("t-stoploss-%s-%d", side, time.Now().Unix()),
	}

	// 组装请求
	order := gateapi.FuturesPriceTriggeredOrder{
		Trigger: trigger,
		Initial: initial,
	}

	// 调用 API
	resp, _, err := t.client.FuturesApi.CreatePriceTriggeredOrder(t.getClientCtx(), settle, order)
	if err != nil {
		return fmt.Errorf("创建止损单失败: %w", err)
	}
	log.Printf("  CreatePriceTriggeredOrder resp %v", resp)
	log.Printf("  止损价设置: %.4f", stopPrice)

	return nil
}

// SetTakeProfit 设置止盈单（基于 price-triggered order）
func (t *GateTrader) SetTakeProfit(symbol string, positionSide string, quantity, takeProfitPrice float64) error {
	settle := "usdt"
	symbol = formatSymbolToContract(symbol)

	// 1️⃣ 参数验证
	side := strings.ToLower(strings.TrimSpace(positionSide))
	if side != "long" && side != "short" {
		return fmt.Errorf("positionSide 必须是 'long' 或 'short'")
	}
	if quantity <= 0 {
		return fmt.Errorf("quantity 必须大于 0")
	}
	if takeProfitPrice <= 0 {
		return fmt.Errorf("takeProfitPrice 必须大于 0")
	}

	// 3️⃣ 确定触发规则与方向
	var rule int32
	if side == "long" {
		// price ≥ takeProfitPrice
		rule = 1
	} else {
		// price ≤ takeProfitPrice
		rule = 2
	}

	// 4️⃣ 构建触发条件
	trigger := gateapi.FuturesPriceTrigger{
		Price:     fmt.Sprintf("%f", takeProfitPrice),
		Rule:      rule, // 1: <=, 2: >=
		PriceType: 1,    // 标记价触发 mark_price
		// Expiration:   86400, // 有效期 1 天
		StrategyType: 0,
	}

	// 5️⃣ 构建触发后的订单参数
	initial := gateapi.FuturesInitialOrder{
		Contract: symbol,
		Size:     0,
		Price:    "0",   // 市价单
		Tif:      "ioc", // 立即成交
		Close:    true,  // 平仓
		Text:     fmt.Sprintf("t-takeprofit-%s-%d", side, time.Now().Unix()),
	}

	// 6️⃣ 创建止盈触发单
	order := gateapi.FuturesPriceTriggeredOrder{
		Trigger: trigger,
		Initial: initial,
	}

	resp, _, err := t.client.FuturesApi.CreatePriceTriggeredOrder(t.getClientCtx(), settle, order)
	if err != nil {
		return fmt.Errorf("创建止盈单失败: %w", err)
	}
	log.Printf("  CreatePriceTriggeredOrder resp %v", resp)
	log.Printf("  止盈价设置: %.4f", takeProfitPrice)
	return nil
}
