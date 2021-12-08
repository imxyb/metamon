package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-telegram-bot-api/telegram-bot-api"
	"github.com/imroc/req"
	"github.com/jmcvetta/randutil"
	"github.com/panjf2000/ants/v2"
	"github.com/urfave/cli/v2"
	"go.uber.org/atomic"
)

var noTearErr = errors.New("体力耗尽")
var noPayErr = errors.New("raca费用不足")
var noNeedUpdateLevel = errors.New("升级物品不足/经验不足，尚未可以升级")

var (
	accessToken string
	fromAddress string
	poolNum     = 5
)

var (
	proxys []string
	l      sync.Mutex
	ready  = make(chan bool)
	once   sync.Once
)

type RoundTrip struct {
	base http.Transport
}

func (r *RoundTrip) RoundTrip(request *http.Request) (*http.Response, error) {
	l.Lock()
	proxy, _ := randutil.ChoiceString(proxys)
	l.Unlock()
	proxyUrl, _ := url.Parse(fmt.Sprintf("http://%s", proxy))
	r.base.Proxy = http.ProxyURL(proxyUrl)
	return r.base.RoundTrip(request)
}

type PYResult struct {
	Code int `json:"code"`
	Data []struct {
		IP         string `json:"ip"`
		Port       int    `json:"port"`
		ExpireTime string `json:"expire_time"`
	} `json:"data"`
	Msg     string `json:"msg"`
	Success bool   `json:"success"`
}

func switchProxy() {
	for {
		l.Lock()
		proxys = nil
		resp, err := http.Get(
			"http://tiqu.pyhttp.taolop.com/getip?count=54&neek=14284&type=2&yys=0&port=1&sb=&mr=1&sep=0&ts=1&pack=9529",
		)
		if err != nil {
			l.Unlock()
			fmt.Println(err)
			continue
		}
		b, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			l.Unlock()
			fmt.Println(err)
			continue
		}
		var pyresult PYResult
		if err = json.Unmarshal(b, &pyresult); err != nil {
			l.Unlock()
			fmt.Println(err)
			continue
		}
		if pyresult.Code != 0 {
			l.Unlock()
			fmt.Printf("pin yin code:%d", pyresult.Code)
			continue
		}
		for _, res := range pyresult.Data {
			proxys = append(proxys, fmt.Sprintf("%s:%d", res.IP, res.Port))
		}
		l.Unlock()
		once.Do(
			func() {
				close(ready)
			},
		)
		time.Sleep(60 * time.Second)
	}
}

func main() {
	app := &cli.App{
		Name: "元兽游戏",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "address",
				Usage: "填写钱包地址",
			},
			&cli.StringFlag{
				Name:  "token",
				Usage: "请在login中粘贴你的token",
			},
			&cli.IntFlag{
				Name:  "pool_num",
				Usage: "同时运行n个元兽进行战斗",
			},
		},
		Before: func(context *cli.Context) error {
			go switchProxy()
			select {
			case <-ready:
			case <-time.After(10 * time.Second):
				panic("获取代理超时")
				return nil
			}
			req.SetClient(
				&http.Client{
					Transport: &RoundTrip{},
				},
			)
			fromAddress = context.String("address")
			accessToken = context.String("token")
			if context.IsSet("pool_num") {
				poolNum = context.Int("pool_num")
			}
			return nil
		},
		Commands: []*cli.Command{
			{
				Name: "updatelevel",
				Action: func(c *cli.Context) error {
					if err := updateLevel(); err != nil {
						fmt.Println(err)
						return err
					}
					return nil
				},
			},
			{
				Name: "start",
				Action: func(c *cli.Context) error {
					fmt.Println("开始游戏")
					start()
					fmt.Printf("游戏结束,时间:%s\n", time.Now().Format("2006-01-02 15:04:05"))
					return nil
				},
			},
			{
				Name: "mint",
				Action: func(c *cli.Context) error {
					if err := mint(); err != nil {
						fmt.Println(err)
						return err
					}
					return nil
				},
			},
			{
				Name: "checkbag",
				Action: func(c *cli.Context) error {
					racaCoin, pnum, err := checkBag()
					if err != nil {
						fmt.Println(err)
						return err
					}
					fmt.Println("余额:", racaCoin, "碎片", pnum)
					return nil
				},
			},
			{
				Name: "openegg",
				Action: func(c *cli.Context) error {
					if err := openEgg(); err != nil {
						fmt.Println(err)
						return err
					}
					return nil
				},
			},
		},
	}

	if err := app.Run(os.Args); err != nil {
		return
	}
}

var wg sync.WaitGroup

func battleProcess(total, wins *atomic.Int32, metamon Metamon) {
	defer wg.Done()
	fmt.Printf("metamon %s 开始战斗\n", metamon.ID)
	for {
		bid, err := getBattleObject(metamon.ID, metamon.Level)
		if err != nil {
			fmt.Printf("metamon %s 获取对战对象失败\n", metamon.ID)
			fmt.Println(err)
			continue
		}
		win, err := battle(metamon.ID, bid, metamon.Level)
		if err != nil {
			if err == noTearErr {
				fmt.Printf("metamon %s 没有体力\n", metamon.ID)
				break
			}
			fmt.Printf("metamon %s 没有成功开始战斗,重试\n", metamon.ID)
			fmt.Println(err)
			continue
		}

		total.Add(1)
		if win {
			wins.Add(1)
		}

		racaCoin, _, err := checkBag()
		if err != nil {
			fmt.Println("获取背包失败,", err)
			continue
		}
		if racaCoin < 50 {
			for {
				if racaCoin > 50 {
					fmt.Println("raca余额足够，战斗继续")
					break
				} else {
					fmt.Println("raca 余额不足，请充值")
				}
				racaCoin, _, err = checkBag()
				if err != nil {
					fmt.Println("获取背包失败,", err)
					continue
				}
				time.Sleep(3 * time.Second)
			}
		}
		if err = updateLevelByID(metamon.ID); err != nil {
			if err != noNeedUpdateLevel {
				fmt.Println(err)
			}
			continue
		}
		time.Sleep(2 * time.Second)
	}
}

type Warp struct {
	Total   *atomic.Int32
	Wins    *atomic.Int32
	Metamon Metamon
}

func start() {
	total := atomic.NewInt32(int32(0))
	wins := atomic.NewInt32(int32(0))

	ms, err := getAvailMetaMon()
	if err != nil {
		fmt.Println(err)
		return
	}

	if err != nil {
		fmt.Println(err)
		return
	}
	if len(ms) == 0 {
		fmt.Println("当前没有任何元兽有体力")
		return
	}
	racaCoin, cpum, err := checkBag()
	if racaCoin < 50 {
		fmt.Println("raca代币不够")
		return
	}

	fmt.Printf("当前有%d只元兽有体力\n", len(ms))
	warpBattleProcess := func(i interface{}) {
		warp := i.(*Warp)
		battleProcess(warp.Total, warp.Wins, warp.Metamon)
	}
	p, _ := ants.NewPoolWithFunc(poolNum, warpBattleProcess)
	defer p.Release()
	for _, metamon := range ms {
		wg.Add(1)
		p.Invoke(
			&Warp{
				Total:   total,
				Wins:    wins,
				Metamon: metamon,
			},
		)
	}
	wg.Wait()

	_, pnum, err := checkBag()
	report := fmt.Sprintf(
		"%s 战斗结束，当前碎片数量:%d，今天战斗获取数量:%d, 胜率:%.2f\n", fromAddress, pnum, pnum-cpum,
		float64(wins.Load())/float64(total.Load()),
	)
	fmt.Printf(report)
	reportTG(report)

	fmt.Println("战斗结束,开始mint")
	if err := mint(); err != nil {
		fmt.Println(err)
	}
}

type Metamon struct {
	ID     string `json:"id"`
	Level  int    `json:"level"`
	Exp    int    `json:"exp"`
	ExpMax int    `json:"expMax"`
	Tear   int    `json:"tear"`
}

type GetAllMetaMonResult struct {
	Data struct {
		MetamonList []Metamon
	} `json:"data"`
}

type MetaMonProp struct {
	Data struct {
		Tear int `json:"tear"`
	} `json:"data"`
}

func getAvailMetaMon() ([]Metamon, error) {
	api := "https://metamon-api.radiocaca.com/usm-api/getWalletPropertyList"
	resp, err := req.Post(
		api, req.Param{"address": fromAddress, "pageSize": 300}, req.Header{"accesstoken": accessToken},
	)
	if err != nil {
		return nil, err
	}

	var rs GetAllMetaMonResult
	if resp.Response().StatusCode != 200 {
		return nil, errors.New(fmt.Sprintf("resp.Response().StatusCode:%d", resp.Response().StatusCode))
	}
	r, err := resp.ToString()
	if err != nil {
		return nil, err
	}
	if strings.Contains(r, "user token is invalid") {
		return nil, errors.New("token过期，请更新")
	}
	err = resp.ToJSON(&rs)
	if err != nil {
		return nil, err
	}

	var metamons []Metamon

	for _, meta := range rs.Data.MetamonList {
		if meta.Tear > 0 {
			metamons = append(metamons, meta)
		}
	}

	return metamons, nil
}

type BatterObjResult struct {
	Data struct {
		Objects []struct {
			ID  string `json:"id"`
			Sca int    `json:"sca"`
		}
	}
}

func getBattleObject(metaID string, level int) (int, error) {
	api := "https://metamon-api.radiocaca.com/usm-api/getBattelObjects"
	front := 1
	if level >= 21 && level <= 40 {
		front = 2
	} else if level >= 41 && level <= 60 {
		front = 3
	}
	resp, err := req.Post(
		api,
		req.Param{
			"address":   fromAddress,
			"metamonId": metaID,
			"front":     front,
		},
		req.Header{"accesstoken": accessToken},
	)
	if err != nil {
		return 0, err
	}
	var objs BatterObjResult
	err = resp.ToJSON(&objs)
	if err != nil {
		return 0, err
	}
	m := make(map[int]int)
	var scas []int
	for _, object := range objs.Data.Objects {
		m[object.Sca], _ = strconv.Atoi(object.ID)
		scas = append(scas, object.Sca)
	}
	return m[scas[0]], err
}

type BatterResult struct {
	Code string `json:"code"`
	Data struct {
		BattleLevel      int `json:"battleLevel"`
		BpFragmentNum    int `json:"bpFragmentNum"`
		BpPotionNum      int `json:"bpPotionNum"`
		ChallengeExp     int `json:"challengeExp"`
		ChallengeLevel   int `json:"challengeLevel"`
		ChallengeMonster struct {
			Con           int         `json:"con"`
			ConMax        int         `json:"conMax"`
			CreateTime    string      `json:"createTime"`
			Crg           int         `json:"crg"`
			CrgMax        int         `json:"crgMax"`
			Exp           int         `json:"exp"`
			ExpMax        int         `json:"expMax"`
			ID            string      `json:"id"`
			ImageURL      string      `json:"imageUrl"`
			Inte          int         `json:"inte"`
			InteMax       int         `json:"inteMax"`
			Inv           int         `json:"inv"`
			InvMax        int         `json:"invMax"`
			IsPlay        bool        `json:"isPlay"`
			ItemID        int         `json:"itemId"`
			ItemNum       int         `json:"itemNum"`
			LastOwner     string      `json:"lastOwner"`
			Level         int         `json:"level"`
			LevelMax      int         `json:"levelMax"`
			Life          int         `json:"life"`
			LifeLL        int         `json:"lifeLL"`
			Luk           int         `json:"luk"`
			LukMax        int         `json:"lukMax"`
			MonsterUpdate bool        `json:"monsterUpdate"`
			Owner         string      `json:"owner"`
			Race          string      `json:"race"`
			Rarity        string      `json:"rarity"`
			Sca           int         `json:"sca"`
			ScaMax        int         `json:"scaMax"`
			Status        int         `json:"status"`
			Tear          int         `json:"tear"`
			TokenID       interface{} `json:"tokenId"`
			UpdateTime    string      `json:"updateTime"`
			Years         int         `json:"years"`
		} `json:"challengeMonster"`
		ChallengeMonsterID string `json:"challengeMonsterId"`
		ChallengeNft       struct {
			ContractAddress string      `json:"contractAddress"`
			CreatedAt       string      `json:"createdAt"`
			Description     string      `json:"description"`
			ID              string      `json:"id"`
			ImageURL        string      `json:"imageUrl"`
			Level           interface{} `json:"level"`
			Metadata        string      `json:"metadata"`
			Name            string      `json:"name"`
			Owner           string      `json:"owner"`
			Status          int         `json:"status"`
			Symbol          string      `json:"symbol"`
			TokenID         string      `json:"tokenId"`
			UpdatedAt       string      `json:"updatedAt"`
		} `json:"challengeNft"`
		ChallengeOwner   string `json:"challengeOwner"`
		ChallengeRecords []struct {
			AttackType       int    `json:"attackType"`
			ChallengeID      string `json:"challengeId"`
			DefenceType      int    `json:"defenceType"`
			ID               string `json:"id"`
			MonsteraID       string `json:"monsteraId"`
			MonsteraLife     int    `json:"monsteraLife"`
			MonsteraLifelost int    `json:"monsteraLifelost"`
			MonsterbID       string `json:"monsterbId"`
			MonsterbLife     int    `json:"monsterbLife"`
			MonsterbLifelost int    `json:"monsterbLifelost"`
		} `json:"challengeRecords"`
		ChallengeResult   bool `json:"challengeResult"`
		ChallengedMonster struct {
			Con           int         `json:"con"`
			ConMax        int         `json:"conMax"`
			CreateTime    string      `json:"createTime"`
			Crg           int         `json:"crg"`
			CrgMax        int         `json:"crgMax"`
			Exp           int         `json:"exp"`
			ExpMax        int         `json:"expMax"`
			ID            string      `json:"id"`
			ImageURL      string      `json:"imageUrl"`
			Inte          int         `json:"inte"`
			InteMax       int         `json:"inteMax"`
			Inv           int         `json:"inv"`
			InvMax        int         `json:"invMax"`
			IsPlay        bool        `json:"isPlay"`
			ItemID        int         `json:"itemId"`
			ItemNum       int         `json:"itemNum"`
			LastOwner     string      `json:"lastOwner"`
			Level         int         `json:"level"`
			LevelMax      int         `json:"levelMax"`
			Life          int         `json:"life"`
			LifeLL        int         `json:"lifeLL"`
			Luk           int         `json:"luk"`
			LukMax        int         `json:"lukMax"`
			MonsterUpdate bool        `json:"monsterUpdate"`
			Owner         string      `json:"owner"`
			Race          string      `json:"race"`
			Rarity        string      `json:"rarity"`
			Sca           int         `json:"sca"`
			ScaMax        int         `json:"scaMax"`
			Status        int         `json:"status"`
			Tear          int         `json:"tear"`
			TokenID       interface{} `json:"tokenId"`
			UpdateTime    string      `json:"updateTime"`
			Years         int         `json:"years"`
		} `json:"challengedMonster"`
		ChallengedMonsterID string `json:"challengedMonsterId"`
		ChallengedNft       struct {
			ContractAddress string      `json:"contractAddress"`
			CreatedAt       string      `json:"createdAt"`
			Description     string      `json:"description"`
			ID              string      `json:"id"`
			ImageURL        string      `json:"imageUrl"`
			Level           interface{} `json:"level"`
			Metadata        string      `json:"metadata"`
			Name            string      `json:"name"`
			Owner           string      `json:"owner"`
			Status          int         `json:"status"`
			Symbol          string      `json:"symbol"`
			TokenID         string      `json:"tokenId"`
			UpdatedAt       string      `json:"updatedAt"`
		} `json:"challengedNft"`
		ChallengedOwner string      `json:"challengedOwner"`
		CreateTime      interface{} `json:"createTime"`
		ID              string      `json:"id"`
		MonsterUpdate   bool        `json:"monsterUpdate"`
		UpdateTime      interface{} `json:"updateTime"`
	} `json:"data"`
	ErrorText string `json:"errorText"`
	Message   string `json:"message"`
	Result    int    `json:"result"`
}

func battle(metaIDA string, metaIDB, level int) (bool, error) {
	api := "https://metamon-api.radiocaca.com/usm-api/startBattle"

	batterLevel := 1
	if level >= 21 && level <= 40 {
		batterLevel = 2
	} else if level >= 41 && level <= 60 {
		batterLevel = 3
	}

	resp, err := req.Post(
		api, req.Param{
			"address":     fromAddress,
			"monsterA":    metaIDA,
			"monsterB":    metaIDB,
			"battleLevel": batterLevel,
		},
		req.Header{"accesstoken": accessToken},
	)
	if err != nil {
		return false, err
	}
	var result BatterResult
	err = resp.ToJSON(&result)
	if err != nil {
		return false, err
	}
	if result.Result == 1 {
		return result.Data.ChallengeResult, nil
	}
	if strings.Contains(result.Message, "You didn't pay for the game") {
		return false, noPayErr
	}
	if strings.Contains(result.Message, "Energy") {
		return false, noTearErr
	}
	return false, errors.New("unknown")
}

type BagItem struct {
	Num string `json:"bpNum"`
	Typ int    `json:"bpType"`
}

type Bag struct {
	Data struct {
		Items []BagItem `json:"item"`
	} `json:"data"`
}

func checkBag() (int, int, error) {
	api := "https://metamon-api.radiocaca.com/usm-api/checkBag"
	resp, err := req.Post(
		api, req.Param{
			"address": fromAddress,
		}, req.Header{"accesstoken": accessToken},
	)
	if err != nil {
		return 0, 0, err
	}
	bag := new(Bag)
	if err := resp.ToJSON(&bag); err != nil {
		return 0, 0, err
	}
	var (
		pieceNum int
		racaCoin int
	)
	for _, item := range bag.Data.Items {
		if item.Typ == 1 {
			pieceNum, _ = strconv.Atoi(item.Num)
		}
		if item.Typ == 5 {
			racaCoin, _ = strconv.Atoi(item.Num)
		}
	}
	return racaCoin, pieceNum, nil
}

func updateLevelByID(nftID string) error {
	updateApi := "https://metamon-api.radiocaca.com/usm-api/updateMonster"
	resp, err := req.Post(
		updateApi, req.Param{
			"address": fromAddress,
			"nftId":   nftID,
		}, req.Header{"accesstoken": accessToken},
	)
	if err != nil {
		return err
	}
	result := make(map[string]interface{})
	if err = resp.ToJSON(&result); err != nil {
		return err
	}
	if result["result"].(float64) != -1 {
		fmt.Printf("metamon %s 升级\n", nftID)
		return nil
	}
	return noNeedUpdateLevel
}

func updateLevel() error {
	api := "https://metamon-api.radiocaca.com/usm-api/getWalletPropertyList"
	resp, err := req.Post(api, req.Param{"address": fromAddress}, req.Header{"accesstoken": accessToken})
	if err != nil {
		return err
	}

	var rs GetAllMetaMonResult
	if resp.Response().StatusCode != 200 {
		return errors.New(fmt.Sprintf("resp.Response().StatusCode:%d", resp.Response().StatusCode))
	}
	err = resp.ToJSON(&rs)
	if err != nil {
		return err
	}
	hasUpdateLevel := false
	for _, metamon := range rs.Data.MetamonList {
		updateApi := "https://metamon-api.radiocaca.com/usm-api/updateMonster"
		resp, err := req.Post(
			updateApi, req.Param{
				"address": fromAddress,
				"nftId":   metamon.ID,
			}, req.Header{"accesstoken": accessToken},
		)
		if err != nil {
			return err
		}
		result := make(map[string]interface{})
		if err = resp.ToJSON(&result); err != nil {
			return err
		}
		if result["result"].(float64) != -1 {
			hasUpdateLevel = true
			fmt.Printf("metamon %s 升级\n", metamon.ID)
		}
	}
	if !hasUpdateLevel {
		return errors.New("目前没有任何需要升级的元兽")
	}
	return nil
}

func mint() (err error) {
	for {
		api := "https://metamon-api.radiocaca.com/usm-api/composeMonsterEgg"
		resp, err := req.Post(api, req.Param{"address": fromAddress}, req.Header{"accesstoken": accessToken})
		if err != nil {
			return err
		}

		result := make(map[string]interface{})
		if err = resp.ToJSON(&result); err != nil {
			return err
		}
		code := result["code"].(string)
		if code == "SUCCESS" {
			_, num, err := checkBag()
			if err != nil {
				return err
			}
			fmt.Printf("合蛋成功，剩余碎片:%d\n", num)
			continue
		}

		return errors.New("没有足够碎片合成元兽蛋")
	}
}

type OpenEggResult struct {
	Code string `json:"code"`
	Data struct {
		Amount   int         `json:"amount"`
		Category string      `json:"category"`
		ID       interface{} `json:"id"`
		ImageURL string      `json:"imageUrl"`
		Rarity   interface{} `json:"rarity"`
		Status   bool        `json:"status"`
		TokenID  interface{} `json:"tokenId"`
	} `json:"data"`
	ErrorText string `json:"errorText"`
	Message   string `json:"message"`
	Result    int    `json:"result"`
}

func openEgg() error {
	for {
		api := "https://metamon-api.radiocaca.com/usm-api/openMonsterEgg"
		resp, err := req.Post(api, req.Param{"address": fromAddress}, req.Header{"accesstoken": accessToken})
		if err != nil {
			return err
		}

		var result OpenEggResult
		if err = resp.ToJSON(&result); err != nil {
			return err
		}
		if result.Code == "SUCCESS" {
			fmt.Printf("开蛋完成，开出:%s\n", result.Data.Category)
			continue
		}

		return errors.New("没有可开的元兽蛋")
	}
}

func reportTG(text string) {
	bot, err := tgbotapi.NewBotAPI("2112019534:AAFe3D-MxhzgwL4ubItfZWQ_CulM7gJvx9k")
	if err != nil {
		fmt.Println(err)
		return
	}
	message := tgbotapi.NewMessage(-1001572783511, text)
	bot.Send(message)
}
