package main

//go-dayleft　残り日数を計算し表示する機能

import (
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"time"

	"github.com/mattn/go-runewidth"
	"github.com/mitchellh/go-homedir"
	"github.com/nsf/termbox-go"
)

//Setting アプリケーションの設定
type Setting struct {
	FlushRate int  `json:"FlushRate"` //更新間隔
	MyPlan    Plan `json:"MyPlan"`    //予定を設定する
}

//NewSetting Setting構造体を新規作成する。
func NewSetting() Setting {
	return Setting{}
}

//Plan 目標日付と名前を持つ。また日付にNowと書くと起動日時が書かれる。（その後、設定ファイルは更新される。）
type Plan struct {
	Name       string `json:"Name"`       //名前
	StartDate  string `Json:"StartDate"`  //開始日時
	TargetDate string `json:"TargetDate"` //目標日時

}

//NewPlan 計画の目標日時と開始日時、そして名前を設定する。
func NewPlan(n, start, end string) Plan {
	return Plan{n, start, end}
}

//PlanData Planによく似ているが、内部データをtime.Time型で保持する
type PlanData struct {
	Name       string    //名前
	StartDate  time.Time //開始日時
	TargetDate time.Time //目標日時
}

//NewPlanData 新規作成する。引数はPlan型で内部で変換されて処理される。
func NewPlanData(p *Plan) PlanData {

	name, start, target := p.Name, p.StartDate, p.TargetDate
	var sd, ed time.Time
	if start == "Now" {
		start = time.Now().Format(timeLayoutDate)
		p.StartDate = start //上書き
	}
	if ct, err := time.Parse(timeLayoutDate, start); err != nil {
		//変換失敗
		sd = time.Date(2000, 1, 2, 0, 0, 0, 0, time.Local)
		p.StartDate = sd.Format(timeLayoutDate) //上書き
	} else {
		sd = ct
	}
	if ct, err := time.Parse(timeLayoutDate, target); err != nil {
		//変換失敗
		ed = time.Date(2000, 1, 2, 0, 0, 0, 0, time.Local)
		p.TargetDate = ed.Format(timeLayoutDate) //上書き
	} else {
		ed = ct
	}
	return PlanData{
		Name:       name,
		StartDate:  sd,
		TargetDate: ed,
	}
}

var (
	home, _ = homedir.Dir()
	//SettingFilePath 設定ファイルの取得アドレス
	SettingFilePath = home + "/go-dayleft.json"
	defaultSetting  = Setting{
		//初期設定
		FlushRate: 10,
		MyPlan: Plan{
			Name:       "２０１８年まで",
			StartDate:  "2017/01/01",
			TargetDate: "2018/01/01",
		},
	}

	nowSetting     Setting //アプリケーションの設定
	width, height  int     //画面幅
	stonD, stoeD   int     //目標に対して、現在日時からの日数と開始日時からの日数
	l1, l2, l3, l4 string  //Planについての詳細表示のための行
	timeLayout     = "2006/01/02 15:04:05"
	timeLayoutDate = "2006/01/02"
	weekdayLayout  = [...]string{"日", "月", "火", "水", "木", "金", "土"}
	keyCh          = make(chan termbox.Key, 1) //なんのボタンが押されたかの送受信用
	fCh            = make(chan bool)           //画面更新用チャンネル
	dCh            = make(chan bool)           //日付変更時のチャンネルフラグ
	plans          []PlanData
	lplan          PlanData //現在表示中のPlan
)

func main() {
	//初期化とエラー処理
	fmt.Println("設定ファイルアドレス : " + SettingFilePath)
	Init()           //初期化
	UpdatePlanInfo() //Planに関して計算する。
	go KeyEventLoop()
	go FlushTimer()
	go Today2TomorrowTimer()
	mainLoop()
	defer termbox.Clear(termbox.ColorDefault, termbox.ColorDefault)
	defer termbox.Close()
}

//Init 初期化処理。表示する情報の取得を行う
func Init() {
	//画面サイズだけ取得して一旦終了させる。
	err := termbox.Init()
	if err != nil {
		panic(err)
	}
	width, height = termbox.Size()       //画面比取得
	termbox.Close()                      //一旦終了させる
	runtime.GOMAXPROCS(runtime.NumCPU()) //並列処理
	nowSetting = NewSetting()            //設定構造体作成
	//設定ファイルが存在するか調べる。
	_, err = os.Stat(SettingFilePath)
	if err == nil {
		//設定ファイルが存在する場合
		//設定ファイルから設定を取得。（設定ファイルが設定できなかった場合、デフォルト設定が適用される。
		fmt.Println("設定ファイルを読込中……")
		ls, ok, errmsg := LoadSettingFile()

		if ok == true {
			nowSetting = ls
		} else {
			//設定に不具合があった場合、エラーメッセージを5秒表示
			fmt.Println(errmsg)
			nowSetting = defaultSetting
		}
	} else {
		//設定ファイルが存在しない場合
		fmt.Println("初期設定ファイルを作成。初期設定で起動します。")
		SaveSettingFile(&defaultSetting)
		nowSetting = defaultSetting
	}

	//設定取得処理完了
	fmt.Println("設定を取得しました。起動中。")
	//横幅の設定の偶数奇数判定
	if float64(width%2) != 0 {
		//奇数
		width-- //偶数にする
	}

	//Plan読み込み
	if nowSetting.MyPlan.Name == "" && nowSetting.MyPlan.StartDate == "" && nowSetting.MyPlan.TargetDate == "" {
		//Planの設定がなされていない場合はデフォルトを適用。
		nowSetting.MyPlan = defaultSetting.MyPlan
	}
	plans = []PlanData{NewPlanData(&nowSetting.MyPlan)} //PlanをPlanDataに変換
	lplan = plans[0]                                    //将来複数のPlanを見られるようにするための変数
	SaveSettingFile(&nowSetting)                        //自動設定の結果を上書き
	//すべての初期化が終わったので改めて立ち上げる
	if err = termbox.Init(); err != nil {
		panic(err)
	}
	termbox.Clear(termbox.ColorDefault, termbox.ColorDefault) //画面初期化
}

//ViewUpdate 画面上に文字列を表示する
func ViewUpdate() {
	nowtimeString := time.Now().Format(timeLayout)
	nowtimeString += " (" + weekdayLayout[time.Now().Weekday()] + ")"
	SetLine(0, nowtimeString, termbox.ColorDefault, termbox.ColorDefault)
	SetLine(1, l1, termbox.ColorBlack, termbox.ColorWhite)
	SetLine(2, l2, termbox.ColorDefault, termbox.ColorDefault)
	SetLine(3, l3, termbox.ColorDefault, termbox.ColorDefault)
	SetLine(4, l4, termbox.ColorBlack, termbox.ColorWhite)
}

//UpdatePlanInfo Planのデータを更新する
func UpdatePlanInfo() {

	//Planデータの残り日数とかを計算する。
	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)
	stoeD = int(lplan.TargetDate.Sub(lplan.StartDate).Hours()) / 24 //開始日から終了日までの総日数
	stonD = int(lplan.TargetDate.Sub(today).Hours() / 24)           //現在日時から終了日までの総日数
	if stoeD <= 0 {
		//0以下なら
		stoeD = 1 //強制的に一日を総日数とする。
	}
	if stonD < 0 {
		//0未満（負数）なら
		stonD = 0 //強制的に一日とする。
	}
	l1, l2, l3, l4 = "", "", "", "" //詳細表示用の行を初期化
	//一行分の文字列も余白に追加
	l1 = lplan.Name
	l2 += lplan.StartDate.Format(timeLayoutDate) + "〜" + lplan.TargetDate.Format(timeLayoutDate) + " まで"
	l3 += "残り " + strconv.Itoa(stonD) + " 日 / " + strconv.Itoa(stoeD) + "日"
	l4 += "残り " + strconv.Itoa(int(float64(stonD)/float64(stoeD)*100)) + "％ 1日あたり:" + fmt.Sprint(100/float64(stoeD)) + "%"
}

//主要なループ
func mainLoop() {
	for {
		select {
		case key := <-keyCh:
			if key == termbox.KeyEsc || key == termbox.KeyCtrlC {
				return
			}
		case <-fCh:
			termbox.Clear(termbox.ColorDefault, termbox.ColorDefault)
			ViewUpdate()
			termbox.Flush()
		case <-dCh:
			//日付が変わるとき
			UpdatePlanInfo()
		}
	}
}

//SetLine h=表示する高さ s=表示文字列 f=文字表示色 b=背景色
func SetLine(h int, s string, f, b termbox.Attribute) {
	hi := 0 //文字列を置く位置を示す
	for _, r := range s {
		termbox.SetCell(hi, h, r, f, b)
		hi += runewidth.RuneWidth(r)
	}
}

//KeyEventLoop キー押下を検出するためのループ関数
func KeyEventLoop() {
	for {
		ev := termbox.PollEvent()
		switch ev.Type {
		case termbox.EventKey:
			keyCh <- ev.Key
		}
	}
}

//FlushTimer 画面更新間隔
func FlushTimer() {
	for {
		time.Sleep(time.Duration(nowSetting.FlushRate) * time.Millisecond)
		fCh <- true
	}
}

//Today2TomorrowTimer 日付が変わった時にチャンネルを送信する。
func Today2TomorrowTimer() {
	for {
		tott := time.Now().AddDate(0, 0, 1)                                                  //tomorrow this time 明日のこの時間
		tomorrow := time.Date(tott.Year(), tott.Month(), tott.Day(), 0, 0, 0, 0, time.Local) //明日の午前0時
		time.Sleep(tomorrow.Sub(time.Now()))
		//僅かな誤差があった時のために一応日付を確認する。
		for {
			if time.Now().Day() == tott.Day() {
				break
			}
			time.Sleep(time.Duration(1) * time.Second)
		}
		dCh <- true
	}
}

//SaveSettingFile 設定ファイルを保存する。　引数は保存する構造体。
func SaveSettingFile(sset *Setting) {

	var settingfile *os.File
	var err error

	if _, err = os.Stat(SettingFilePath); err == nil {
		//存在する場合
		settingfile, err = os.OpenFile(SettingFilePath, os.O_WRONLY, 0666)
	} else {
		//存在しない場合
		settingfile, err = os.Create(SettingFilePath)
	}

	if err != nil {
		//エラーが発生した場合
		fmt.Println("設定ファイル取得/作成エラー！")
		fmt.Println(err)
	}

	defer settingfile.Close()

	encoder := json.NewEncoder(settingfile)

	err = encoder.Encode(sset)

	if err != nil {
		//エラーが発生した場合
		fmt.Println("ERROR! JSON保存処理エラー！")
		fmt.Println(err)
	}
}

//LoadSettingFile 設定ファイルをロードする。 戻り値は成否
func LoadSettingFile() (loadSetting Setting, ok bool, errmsg string) {

	loadSetting = Setting{}
	errmsg = "" //エラー時のメッセージ
	ok = true   //構造体がうまく読み込めたのかのフラグ
	if _, err := os.Stat(SettingFilePath); err != nil {
		//存在しない場合
		return Setting{}, false, "ERROR! ファイルが存在しません！"
	}

	settingfile, err := os.OpenFile(SettingFilePath, os.O_RDONLY, 0666)
	if err != nil {
		fmt.Println(err)
		return Setting{}, false, "ERROR! ファイルが開けません！"
	}
	defer settingfile.Close()

	decoder := json.NewDecoder(settingfile)
	err = decoder.Decode(&loadSetting)

	if err != nil {
		fmt.Println(err)
		return Setting{}, false, "ERROR! 設定JSONを読み込めませんでした!"
	}

	//設定が正しくなされているか確認
	switch {
	case loadSetting.FlushRate <= 0:
		errmsg = "ERROR! FlushRateは1以上の値を設定してください。"
		ok = false
	}
	return

}
