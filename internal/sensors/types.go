// Package sensors reads Linux hardware sensors without invoking external
// programs. Missing or unsupported sensors are represented as unavailable
// values rather than as errors.
package sensors

// Sensor units used by Snapshot.
const (
	UnitCelsius = "°C"
	UnitRPM     = "RPM"
	UnitPercent = "%"
	UnitWh      = "Wh"
	UnitW       = "W"
	UnitBytes   = "B"
)

// Temperature is one thermal-zone or hwmon temperature reading.
type Temperature struct {
	Label   string
	Source  string // Path relative to the sysfs root.
	Celsius float64
	Unit    string
}

// Fan is one hwmon fan input. An RPM of zero is a valid stopped fan.
type Fan struct {
	Label  string
	Source string // Path relative to the sysfs root.
	RPM    float64
	Unit   string
}

// Battery contains the values exposed by a BAT* power-supply device. The Has
// fields distinguish an unavailable reading from a genuine zero.
type Battery struct {
	Name         string
	Label        string
	Manufacturer string
	Model        string
	Status       string

	Percent     float64
	PercentUnit string
	HasPercent  bool

	EnergyNow           float64
	EnergyFull          float64
	EnergyFullDesign    float64
	EnergyUnit          string
	HasEnergyNow        bool
	HasEnergyFull       bool
	HasEnergyFullDesign bool

	PowerNow    float64
	PowerUnit   string
	HasPowerNow bool
}

// PowerSupply describes an AC* or ADP* adapter.
type PowerSupply struct {
	Name      string
	Label     string
	Kind      string
	Online    bool
	HasOnline bool
}

// CPUUsage is aggregate utilization across all CPUs. Available is false for a
// sampler's first reading, a missing /proc/stat, or a counter reset.
type CPUUsage struct {
	Label       string
	Utilization float64
	Unit        string
	Available   bool
}

// MemoryUsage reports memory in bytes. UsedPercent is a percentage.
type MemoryUsage struct {
	Label          string
	TotalBytes     uint64
	AvailableBytes uint64
	UsedBytes      uint64
	UsedPercent    float64
	Unit           string
	PercentUnit    string
	Available      bool
}

// Snapshot is a point-in-time view of the available sensors. CPUTemperatures
// and DeviceTemperatures together contain every valid thermal-zone and hwmon
// temperature input. CPUTemperature, when non-nil, is the hottest CPU sensor.
type Snapshot struct {
	CPUTemperature     *Temperature
	CPUTemperatures    []Temperature
	DeviceTemperatures []Temperature
	Fans               []Fan
	Batteries          []Battery
	PowerSupplies      []PowerSupply
	CPU                CPUUsage
	Memory             MemoryUsage
}
