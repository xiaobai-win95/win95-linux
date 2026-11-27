package sensors

import (
	"path/filepath"
	"strings"
)

func readPowerSupplies(sysRoot string) ([]Battery, []PowerSupply, error) {
	root := filepath.Join(sysRoot, "class", "power_supply")
	dirs, err := sensorSubdirs(root, "")
	if err != nil {
		return nil, nil, err
	}
	var batteries []Battery
	var supplies []PowerSupply
	for _, dir := range dirs {
		name := filepath.Base(dir)
		switch {
		case strings.HasPrefix(name, "BAT"):
			batteries = append(batteries, readBattery(dir))
		case strings.HasPrefix(name, "AC"), strings.HasPrefix(name, "ADP"):
			supplies = append(supplies, readAdapter(dir))
		}
	}
	return batteries, supplies, nil
}

func readBattery(dir string) Battery {
	name := filepath.Base(dir)
	battery := Battery{
		Name:         name,
		Label:        name,
		Manufacturer: readText(filepath.Join(dir, "manufacturer_name")),
		Model:        readText(filepath.Join(dir, "model_name")),
		Status:       readText(filepath.Join(dir, "status")),
		PercentUnit:  UnitPercent,
		EnergyUnit:   UnitWh,
		PowerUnit:    UnitW,
	}
	if battery.Model != "" {
		battery.Label = battery.Model
	}

	voltage, _ := readNumber(filepath.Join(dir, "voltage_now"))
	energyNow, hasEnergyNow := readNonNegativeNumber(filepath.Join(dir, "energy_now"))
	energyFull, hasEnergyFull := readNonNegativeNumber(filepath.Join(dir, "energy_full"))
	energyFullDesign, hasEnergyFullDesign := readNonNegativeNumber(filepath.Join(dir, "energy_full_design"))
	chargeNow, hasChargeNow := readNonNegativeNumber(filepath.Join(dir, "charge_now"))
	chargeFull, hasChargeFull := readNonNegativeNumber(filepath.Join(dir, "charge_full"))
	chargeFullDesign, hasChargeFullDesign := readNonNegativeNumber(filepath.Join(dir, "charge_full_design"))

	// Charge is reported in microamp-hours and voltage in microvolts. Their
	// product is picowatt-hours, or 1e-6 microwatt-hours.
	const chargeVoltageToMicroWh = 1e-6
	if voltage > 0 {
		if !hasEnergyNow && hasChargeNow {
			energyNow = chargeNow * voltage * chargeVoltageToMicroWh
			hasEnergyNow = true
		}
		if !hasEnergyFull && hasChargeFull {
			energyFull = chargeFull * voltage * chargeVoltageToMicroWh
			hasEnergyFull = true
		}
		if !hasEnergyFullDesign && hasChargeFullDesign {
			energyFullDesign = chargeFullDesign * voltage * chargeVoltageToMicroWh
			hasEnergyFullDesign = true
		}
	}

	// Energy attributes are in microwatt-hours and power attributes in
	// microwatts.
	const micro = 1e-6
	if hasEnergyNow {
		battery.EnergyNow = energyNow * micro
		battery.HasEnergyNow = true
	}
	if hasEnergyFull {
		battery.EnergyFull = energyFull * micro
		battery.HasEnergyFull = true
	}
	if hasEnergyFullDesign {
		battery.EnergyFullDesign = energyFullDesign * micro
		battery.HasEnergyFullDesign = true
	}
	if power, ok := readNumber(filepath.Join(dir, "power_now")); ok {
		battery.PowerNow = power * micro
		battery.HasPowerNow = true
	} else if current, ok := readNumber(filepath.Join(dir, "current_now")); ok && voltage > 0 {
		// Microamps times microvolts is picowatts.
		battery.PowerNow = current * voltage * 1e-12
		battery.HasPowerNow = true
	}

	if percent, ok := readNumber(filepath.Join(dir, "capacity")); ok && percent >= 0 && percent <= 100 {
		battery.Percent = percent
		battery.HasPercent = true
	} else if hasEnergyFull && energyFull > 0 && hasEnergyNow {
		battery.Percent = clampPercent(energyNow / energyFull * 100)
		battery.HasPercent = true
	} else if hasChargeFull && chargeFull > 0 && hasChargeNow {
		battery.Percent = clampPercent(chargeNow / chargeFull * 100)
		battery.HasPercent = true
	}
	return battery
}

func readAdapter(dir string) PowerSupply {
	name := filepath.Base(dir)
	kind := "AC"
	if strings.HasPrefix(name, "ADP") {
		kind = "ADP"
	}
	if supplyType := readText(filepath.Join(dir, "type")); supplyType != "" {
		kind = supplyType
	}
	label := readText(filepath.Join(dir, "model_name"))
	if label == "" {
		label = name
	}
	supply := PowerSupply{
		Name:  name,
		Label: label,
		Kind:  kind,
	}
	if online, ok := readNumber(filepath.Join(dir, "online")); ok && (online == 0 || online == 1) {
		supply.Online = online == 1
		supply.HasOnline = true
	}
	return supply
}

func readNonNegativeNumber(path string) (float64, bool) {
	value, ok := readNumber(path)
	return value, ok && value >= 0
}

func clampPercent(percent float64) float64 {
	if percent < 0 {
		return 0
	}
	if percent > 100 {
		return 100
	}
	return percent
}
