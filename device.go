package venstar

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"strings"
)

type Device struct {
	BaseURL *url.URL
	Name string
	Header http.Header
	client *http.Client
}

func NewDevice(msg []byte) (*Device, error) {
	br := bufio.NewReader(bytes.NewReader(msg))
	resp, err := http.ReadResponse(br, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.Header.Get("ST") != "venstar:thermostat:ecp" {
		return nil, nil
	}
	u, err := url.Parse(resp.Header.Get("Location"))
	if err != nil {
		return nil, err
	}
	usn := strings.Split(resp.Header.Get("USN"), ":")
	var name string
	for i, part := range usn {
		if strings.ToLower(part) == "name" && i < len(usn) - 1 {
			name = usn[i+1]
			break
		}
	}
	return &Device{
		BaseURL: u,
		Name: name,
		Header: resp.Header,
		client: &http.Client{},
	}, nil
}

func (dev *Device) String() string {
	return fmt.Sprintf("%s: %s", dev.Name, dev.BaseURL.String())
}

func (dev *Device) get(path []string, obj any) error {
	res, err := dev.client.Get(dev.BaseURL.JoinPath(path...).String())
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return errors.New(res.Status)
	}
	dec := json.NewDecoder(res.Body)
	return dec.Decode(obj)
}

func (dev *Device) post(path []string, obj any) error {
	rv := reflect.ValueOf(obj)
	rt := rv.Type()
	vals := url.Values{}
	for rt.Kind() == reflect.Ptr {
		if rv.IsNil() {
			return errors.New("nil input")
		}
		rv = rv.Elem()
		rt = rt.Elem()
	}
	if rt.Kind() != reflect.Struct {
		return errors.New("input is not a struct")
	}
	for i := 0; i < rt.NumField(); i++ {
		sf := rt.Field(i)
		if sf.PkgPath != "" {
			continue
		}
		var name string
		var omitempty bool
		tag := sf.Tag.Get("json")
		if tag == "" {
			name = toSnakeCase(sf.Name)
		} else {
			parts := strings.Split(sf.Tag.Get("json"), ",")
			name = parts[0]
			if name == "-" {
				continue
			}
			if len(parts) > 1 {
				for _, part := range parts[1:] {
					if part == "omitempty" {
						omitempty = true
					}
				}
			}
		}
		switch sf.Type.Kind() {
		case reflect.String:
			s := rv.Field(i).String()
			if omitempty && s == "" {
				continue
			}
			vals.Add(name, s)
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			iv := rv.Field(i).Int()
			if omitempty && iv == 0 {
				continue
			}
			vals.Add(name, strconv.FormatInt(iv, 10))
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			uv := rv.Field(i).Uint()
			if omitempty && uv == 0 {
				continue
			}
			vals.Add(name, strconv.FormatUint(uv, 10))
		case reflect.Float64, reflect.Float32:
			fv := rv.Field(i).Float()
			if omitempty && fv == 0 {
				continue
			}
			vals.Add(name, strconv.FormatFloat(fv, 'f', -1, 64))
		case reflect.Bool:
			bv := rv.Field(i).Bool()
			if omitempty && !bv {
				continue
			}
			vals.Add(name, strconv.FormatBool(bv))
		default:
			return fmt.Errorf("unsupported field %s type %T", sf.Name, rv.Field(i).Interface())
		}
	}
	res, err := dev.client.PostForm(dev.BaseURL.JoinPath(path...).String(), vals)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return errors.New(res.Status)
	}
	var status StatusResponse
	dec := json.NewDecoder(res.Body)
	err = dec.Decode(&status)
	if err != nil {
		return err
	}
	if status.Error {
		return errors.New(status.Reason)
	}
	return nil
}

func (dev *Device) Info() (*DeviceInfo, error) {
	info := &DeviceInfo{}
	err := dev.get([]string{"query", "info"}, info)
	if err != nil {
		return nil, err
	}
	return info, nil
}

func (dev *Device) Sensors() (map[string]*SensorInfo, error) {
	var resp SensorsResponse
	err := dev.get([]string{"query", "sensors"}, &resp)
	if err != nil {
		return nil, err
	}
	sensors := map[string]*SensorInfo{}
	for _, info := range resp.Sensors {
		sensors[info.Name] = info
	}
	return sensors, nil
}

func (dev *Device) Alerts() (map[string]*AlertInfo, error) {
	var resp AlertsResponse
	err := dev.get([]string{"query", "alerts"}, &resp)
	if err != nil {
		return nil, err
	}
	alerts := map[string]*AlertInfo{}
	for _, info := range resp.Alerts {
		alerts[info.Name] = info
	}
	return alerts, nil
}

func (dev *Device) Runtimes() ([]*RuntimeInfo, error) {
	var resp RuntimesResponse
	err := dev.get([]string{"query", "runtimes"}, &resp)
	if err != nil {
		return nil, err
	}
	return resp.Runtimes, nil
}

func (dev *Device) SetMode(mode ThermostatMode) error {
	info, err := dev.Info()
	if err != nil {
		return fmt.Errorf("error getting current settings: %w", err)
	}
	msg := info.ControlMessage().WithMode(mode)
	err = msg.Validate()
	if err != nil {
		return err
	}
	return dev.post([]string{"control"}, msg)
}

func (dev *Device) SetFanMode(mode FanSetting) error {
	info, err := dev.Info()
	if err != nil {
		return fmt.Errorf("error getting current settings: %w", err)
	}
	msg := info.ControlMessage().WithFan(mode)
	err = msg.Validate()
	if err != nil {
		return err
	}
	return dev.post([]string{"control"}, msg)
}

func (dev *Device) SetHeatTemp(temp float64) error {
	info, err := dev.Info()
	if err != nil {
		return fmt.Errorf("error getting current settings: %w", err)
	}
	mode := info.Mode
	switch mode {
	case ModeAuto, ModeHeat:
	default:
		mode = ModeHeat
	}
	msg := info.ControlMessage().WithMode(mode).WithHeatTemp(temp)
	err = msg.Validate()
	if err != nil {
		return err
	}
	return dev.post([]string{"control"}, msg)
}

func (dev *Device) SetCoolTemp(temp float64) error {
	info, err := dev.Info()
	if err != nil {
		return fmt.Errorf("error getting current settings: %w", err)
	}
	mode := info.Mode
	switch mode {
	case ModeAuto, ModeCool:
	default:
		mode = ModeCool
	}
	msg := info.ControlMessage().WithMode(mode).WithCoolTemp(temp)
	err = msg.Validate()
	if err != nil {
		return err
	}
	return dev.post([]string{"control"}, msg)
}

func (dev *Device) SetHeatCoolTemps(heat, cool float64) error {
	if heat > cool {
		heat, cool = cool, heat
	}
	info, err := dev.Info()
	if err != nil {
		return fmt.Errorf("error getting current settings: %w", err)
	}
	msg := info.ControlMessage().WithMode(ModeAuto).WithHeatTemp(heat).WithCoolTemp(cool)
	err = msg.Validate()
	if err != nil {
		return err
	}
	return dev.post([]string{"control"}, msg)
}

func (dev *Device) SetTempUnits(units TempUnits) error {
	info, err := dev.Info()
	if err != nil {
		return fmt.Errorf("error getting current settings: %w", err)
	}
	msg := info.SettingsMessage().WithTempUnits(units)
	return dev.post([]string{"settings"}, msg)
}

func (dev *Device) SetAway(away AwayState) error {
	info, err := dev.Info()
	if err != nil {
		return fmt.Errorf("error getting current settings: %w", err)
	}
	msg := info.SettingsMessage().WithAway(away)
	return dev.post([]string{"settings"}, msg)
}

func (dev *Device) SetSchedule(sched ScheduleState) error {
	info, err := dev.Info()
	if err != nil {
		return fmt.Errorf("error getting current settings: %w", err)
	}
	msg := info.SettingsMessage().WithSchedule(sched)
	return dev.post([]string{"settings"}, msg)
}

func (dev *Device) SetHumidifySetpoint(setpoint float64) error {
	info, err := dev.Info()
	if err != nil {
		return fmt.Errorf("error getting current settings: %w", err)
	}
	msg := info.SettingsMessage().WithHumidifySetpoint(setpoint)
	return dev.post([]string{"settings"}, msg)
}

func (dev *Device) SetDehumidifySetpoint(setpoint float64) error {
	info, err := dev.Info()
	if err != nil {
		return fmt.Errorf("error getting current settings: %w", err)
	}
	msg := info.SettingsMessage().WithDehumidifySetpoint(setpoint)
	return dev.post([]string{"settings"}, msg)
}

type ThermostatMode int
const (
	ModeOff ThermostatMode = iota
	ModeHeat
	ModeCool
	ModeAuto
)
var thermostatModeNames = map[ThermostatMode]string{
	ModeOff: "off",
	ModeHeat: "heat",
	ModeCool: "cool",
	ModeAuto: "auto",
}

func (mode ThermostatMode) String() string {
	s, ok := thermostatModeNames[mode]
	if !ok {
		return fmt.Sprintf("ThermostatMode%d", mode)
	}
	return s
}

type ThermostatState int
const (
	StateIdle ThermostatState = iota
	StateHeating
	StateCooling
	StateLockout
	StateError
)
var thermostatStateNames = map[ThermostatState]string{
	StateIdle: "idle",
	StateHeating: "heating",
	StateCooling: "cooling",
	StateLockout: "lockout",
	StateError: "error",
}

func (state ThermostatState) String() string {
	s, ok := thermostatStateNames[state]
	if !ok {
		return fmt.Sprintf("ThermostatState%d", state)
	}
	return s
}

type DemandStage int
const (
	StageOff DemandStage = iota
	StageHeating1
	StageHeating2
	StageCooling1
	StageCooling2
)
var demandStageNames = map[DemandStage]string{
	StageOff: "off",
	StageHeating1: "heating1",
	StageHeating2: "heating2",
	StageCooling1: "cooling1",
	StageCooling2: "cooling2",
}

func (stage DemandStage) String() string {
	s, ok := demandStageNames[stage]
	if !ok {
		return fmt.Sprintf("DemandStage%d", stage)
	}
	return s
}

type FanSetting int
const (
	FanSettingAuto FanSetting = iota
	FanSettingOn
)
var fanSettingNames = map[FanSetting]string{
	FanSettingAuto: "auto",
	FanSettingOn: "on",
}

func (fan FanSetting) String() string {
	s, ok := fanSettingNames[fan]
	if !ok {
		return fmt.Sprintf("FanSetting%d", fan)
	}
	return s
}

type FanState int
const (
	FanStateOff FanState = iota
	FanStateOn
)
var fanStateNames = map[FanState]string{
	FanStateOff: "off",
	FanStateOn: "on",
}

func (fan FanState) String() string {
	s, ok := fanStateNames[fan]
	if !ok {
		return fmt.Sprintf("FanState%d", fan)
	}
	return s
}

type TempUnits int
const (
	Fahrenheit TempUnits = iota
	Celsius
)
var tempUnitsNames = map[TempUnits]string{
	Fahrenheit: "°F",
	Celsius: "°C",
}

func (units TempUnits) String() string {
	s, ok := tempUnitsNames[units]
	if !ok {
		return fmt.Sprintf("TempUnits%d", units)
	}
	return s
}

type ScheduleState int
const (
	ScheduleDisabled ScheduleState = iota
	ScheduleEnabled
)
var scheduleStateNames = map[ScheduleState]string{
	ScheduleDisabled: "disabled",
	ScheduleEnabled: "enabled",
}

func (state ScheduleState) String() string {
	s, ok := scheduleStateNames[state]
	if !ok {
		return fmt.Sprintf("ScheduleState%d", state)
	}
	return s
}

type SchedulePart int
const (
	SchedulePartMorning SchedulePart = iota
	SchedulePartDay
	SchedulePartEvening
	SchedulePartNight
	SchedulePartInactive SchedulePart = 255
)
var schedulePartNames = map[SchedulePart]string{
	SchedulePartMorning: "morning",
	SchedulePartDay: "day",
	SchedulePartEvening: "evening",
	SchedulePartNight: "night",
	SchedulePartInactive: "inactive",
}

func (part SchedulePart) String() string {
	s, ok := schedulePartNames[part]
	if !ok {
		return fmt.Sprintf("SchedulePart%d", part)
	}
	return s
}

type AwayState int
const (
	AwayStateHome AwayState = iota
	AwayStateAway
)
var awayStateNames = map[AwayState]string{
	AwayStateHome: "home",
	AwayStateAway: "away",
}

func (state AwayState) String() string {
	s, ok := awayStateNames[state]
	if !ok {
		return fmt.Sprintf("AwayState%d", state)
	}
	return s
}

type HolidayState int
const (
	HolidayStateNotHoliday HolidayState = iota
	HolidayStateHoliday
)
var holidayStateNames = map[HolidayState]string{
	HolidayStateNotHoliday: "regular",
	HolidayStateHoliday: "holiday",
}

func (state HolidayState) String() string {
	s, ok := holidayStateNames[state]
	if !ok {
		return fmt.Sprintf("HolidayState%d", state)
	}
	return s
}

type OverrideState int
const (
	OverrideStateOff OverrideState = iota
	OverrideStateOn
)
var overrideStateNames = map[OverrideState]string{
	OverrideStateOff: "off",
	OverrideStateOn: "on",
}

func (state OverrideState) String() string {
	s, ok := overrideStateNames[state]
	if !ok {
		return fmt.Sprintf("OverrideState%d", state)
	}
	return s
}

type ForceUnoccState int
const (
	ForceUnoccOff ForceUnoccState = iota
	ForceUnoccOn
)
var forceUnoccStateNames = map[ForceUnoccState]string{
	ForceUnoccOff: "off",
	ForceUnoccOn: "on",
}

func (state ForceUnoccState) String() string {
	s, ok := forceUnoccStateNames[state]
	if !ok {
		return fmt.Sprintf("ForceUnoccState%d", state)
	}
	return s
}

type HumidifierState int
const (
	HumidifierOff HumidifierState = iota
	HumidifierOn
)
var humidifierStateNames = map[HumidifierState]string{
	HumidifierOff: "off",
	HumidifierOn: "on",
}

func (state HumidifierState) String() string {
	s, ok := humidifierStateNames[state]
	if !ok {
		return fmt.Sprintf("HumidifierState%d", state)
	}
	return s
}

type AvailableModes int
const (
	AvailableModeAll AvailableModes = iota
	AvailableModeHeatCool
	AvailableModeHeat
	AvailableModeCool
)
var availableModesNames = map[AvailableModes]string{
	AvailableModeAll: "all",
	AvailableModeHeatCool: "heat/cool",
	AvailableModeHeat: "heat",
	AvailableModeCool: "cool",
}

func (mode AvailableModes) String() string {
	s, ok := availableModesNames[mode]
	if !ok {
		return fmt.Sprintf("AvailableModes%d", mode)
	}
	return s
}

type DeviceInfo struct {
	Name               string          `json:"name"`
	Mode               ThermostatMode  `json:"mode"`
	State              ThermostatState `json:"state"`
	ActiveStage        DemandStage     `json:"activestage"`
	FanSetting         FanSetting      `json:"fan"`
	FanState           FanState        `json:"fanstate"`
	TempUnits          TempUnits       `json:"tempunits"`
	Schedule           ScheduleState   `json:"schedule"`
	SchedulePart       SchedulePart    `json:"schedulepart"`
	Away               AwayState       `json:"away"`
	Holiday            HolidayState    `json:"holiday"`
	Override           OverrideState   `json:"override"`
	OverrideTime       int             `json:"overridetime"`
	ForceUnocc         ForceUnoccState `json:"forceunocc"`
	SpaceTemp          float64         `json:"spacetemp"`
	HeatTemp           float64         `json:"heattemp"`
	CoolTemp           float64         `json:"cooltemp"`
	CoolTempMin        float64         `json:"cooltempmin"`
	CoolTempMax        float64         `json:"cooltempmax"`
	HeatTempMin        float64         `json:"heattempmin"`
	HeatTempMax        float64         `json:"heattempmin"`
	SetPointDelta      float64         `json:"setpointdelta"`
	Humidity           float64         `json:"hum"`
	HumidifySetpoint   float64         `json:"hum_setpoint"`
	DehumidifySetpoint float64         `json:"dehum_setpoint"`
	Humidifier         HumidifierState `json:"hum_active"`
	AvailableModes     AvailableModes  `json:"availablemodes"`
//{"name":"THERMOSTAT","mode":3,"state":1,"activestage":1,"fan":0,"fanstate":0,"tempunits":0,"schedule":0,"schedulepart":0,"holiday":0,"override":0,"overridetime":0,"forceunocc":0,"spacetemp":71.0,"heattemp":73.0,"cooltemp":81.0,"cooltempmin":35.0,"cooltempmax":99.0,"heattempmin":35.0,"heattempmax":99.0,"setpointdelta":2,"availablemodes":0}
}

func (info *DeviceInfo) ControlMessage() ControlMessage {
	return ControlMessage{
		Mode:     info.Mode,
		Fan:      info.FanSetting,
		HeatTemp: info.HeatTemp,
		CoolTemp: info.CoolTemp,
	}
}

func (info *DeviceInfo) SettingsMessage() SettingsMessage {
	return SettingsMessage{
		TempUnits:          info.TempUnits,
		Away:               info.Away,
		Schedule:           info.Schedule,
		HumidifySetpoint:   info.HumidifySetpoint,
		DehumidifySetpoint: info.DehumidifySetpoint,
	}
}

type SensorType string
const (
	SensorTypeOutdoor SensorType = "Outdoor"
	SensorTypeReturn  SensorType = "Return"
	SensorTypeRemote  SensorType = "Remote"
	SensorTypeSupply  SensorType = "Supply"
)

type SensorInfo struct {
	Name             string     `json:"name"`
	Temp             float64    `json:"temp"`
	Humidity         float64    `json:"hum"`
	Light            float64    `json:"intensity"`
	IndoorAirQuality float64    `json:"iaq"`
	CO2PPM           float64    `json:"co2"`
	Battery          float64    `json:"battery"`
	Type             SensorType `json:"type"`
}

type SensorsResponse struct {
	Sensors []*SensorInfo `json:"sensors"`
//{"sensors": [{"name":"Thermostat","temp":71},{"name":"Space Temp","temp":71}]}
}

type RuntimeInfo struct {
	Timestamp       float64 `json:"ts"`
	Heat            float64 `json:"heat"`
	HeatStage1      float64 `json:"heat1"`
	HeatStage2      float64 `json:"heat2"`
	Cool            float64 `json:"cool"`
	CoolStage1      float64 `json:"cool1"`
	CoolStage2      float64 `json:"cool2"`
	AuxiliaryStage1 float64 `json:"aux1"`
	AuxiliaryStage2 float64 `json:"aux2"`
	FreeCooling     float64 `json:"fc"`
	Override        float64 `json:"ov"`
	FilterHours     float64 `json:"filterHours"`
	FilterDays      float64 `json:"filterDays"`
}

type RuntimesResponse struct {
	Runtimes []*RuntimeInfo `json:"runtimes"`
//{"runtimes":[{"ts": 1543846745,"heat": 77,"heat1": 74,"heat2": 3,"cool": 0,"cool1": 0,"cool2": 0,"ov": 0,"filterHours": 1,"filterDays": 0}]}
}

type AlertInfo struct {
	Name   string `json:"name"`
	Active bool   `json:"active"`
}

type AlertsResponse struct {
	Alerts []*AlertInfo `json:"alerts"`
//{"alerts": [{"name": "Air Filter","active": false},{"name": "indoorHi","active": false},{"name": "indoorLo","active": false},{"name": "supplyHt","active": false},{"name": "supplyCl","active": false},{"name": "dryCtct","active": false},{"name": "dayHeat","active": false},{"name": "dayCool","active": false},{"name": "filterHr","active": false},{"name": "filter","active": false},{"name": "service","active": false}]}
}

type ControlMessage struct {
	Mode     ThermostatMode `json:"mode"`
	Fan      FanSetting     `json:"fan"`
	HeatTemp float64        `json:"heattemp"`
	CoolTemp float64        `json:"cooltemp"`
}

func (msg ControlMessage) WithMode(mode ThermostatMode) ControlMessage {
	msg.Mode = mode
	return msg
}

func (msg ControlMessage) WithFan(mode FanSetting) ControlMessage {
	msg.Fan = mode
	return msg
}

func (msg ControlMessage) WithHeatTemp(temp float64) ControlMessage {
	msg.HeatTemp = temp
	return msg
}

func (msg ControlMessage) WithCoolTemp(temp float64) ControlMessage {
	msg.CoolTemp = temp
	return msg
}

func (msg ControlMessage) Validate() error {
	if msg.CoolTemp - msg.HeatTemp < 2 {
		return fmt.Errorf("difference between heat & cool temps (%f) less than 2 degrees", msg.CoolTemp - msg.HeatTemp)
	}
	return nil
}

type SettingsMessage struct {
	TempUnits          TempUnits     `json:"tempunits"`
	Away               AwayState     `json:"away"`
	Schedule           ScheduleState `json:"schedule"`
	HumidifySetpoint   float64       `json:"hum_setpoint"`
	DehumidifySetpoint float64       `json:"dehum_setpoint"`
}

func (msg SettingsMessage) WithTempUnits(units TempUnits) SettingsMessage {
	msg.TempUnits = units
	return msg
}

func (msg SettingsMessage) WithAway(away AwayState) SettingsMessage {
	msg.Away = away
	return msg
}

func (msg SettingsMessage) WithSchedule(sched ScheduleState) SettingsMessage {
	msg.Schedule = sched
	return msg
}

func (msg SettingsMessage) WithHumidifySetpoint(setpoint float64) SettingsMessage {
	msg.HumidifySetpoint = setpoint
	return msg
}

func (msg SettingsMessage) WithDehumidifySetpoint(setpoint float64) SettingsMessage {
	msg.DehumidifySetpoint = setpoint
	return msg
}

type StatusResponse struct {
	Success bool `json:"success"`
	Error bool `json:"error"`
	Reason string `json:"reason"`
}
