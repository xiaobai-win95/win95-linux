package desktop

import (
	"math"

	"win95linux/internal/ui"
)

// The first-person view is a real volumetric renderer: camera rays walk the
// voxel grid with a DDA loop, so blocks have depth, occlusion and perspective
// on all three axes. The 2D side view remains available with V as a
// cross-section of the same world.

type bwHit struct {
	ok         bool
	x, y, z    int
	px, py, pz int
	dist       float64
}

func (a *BlockWorldApp) eye() (float64, float64, float64) {
	return a.px + bwPW/2, a.py + 4, a.pz + bwPW/2
}

func (a *BlockWorldApp) lookDir() (float64, float64, float64) {
	cp, sp := math.Cos(a.pitch), math.Sin(a.pitch)
	cy, sy := math.Cos(a.yaw), math.Sin(a.yaw)
	return cy * cp, sp, sy * cp
}

func (a *BlockWorldApp) openYaw(x, y int) float64 {
	yaw, _ := a.openView(x, y)
	return yaw
}

// openView scores the four compass directions by how far the eye can see over
// the terrain, favouring high ground, so spawning opens onto the landscape
// instead of the face of a nearby hill.
func (a *BlockWorldApp) openView(x, y int) (float64, int) {
	bestYaw, bestScore := 0.0, -1
	for i := 0; i < 4; i++ {
		yaw := float64(i) * math.Pi / 2
		cy, sy := math.Cos(yaw), math.Sin(yaw)
		open := 0
		for d := 1; d <= 24; d++ {
			px := x + int(math.Round(cy*float64(d)))
			pz := bwMidZ + int(math.Round(sy*float64(d)))
			blocked := false
			for dy := 1; dy <= 4; dy++ {
				if a.solidTile3(px, y-dy, pz) {
					blocked = true
					break
				}
			}
			if blocked {
				break
			}
			open = d
		}
		score := open*8 + y
		if score > bestScore {
			bestScore, bestYaw = score, yaw
		}
	}
	return bestYaw, bestScore
}

func (a *BlockWorldApp) solidTile3(tx, ty, tz int) bool {
	if tx < 0 || tx >= a.world.W || tz < 0 || tz >= a.world.D {
		return true
	}
	if ty < 0 {
		return false
	}
	if ty >= a.world.H {
		return true
	}
	return bwIsSolid(a.world.At3(tx, ty, tz))
}

func (a *BlockWorldApp) collides3(x, y, z float64) bool {
	x0 := int(math.Floor(x / bwTile))
	x1 := int(math.Floor((x + bwPW - 0.001) / bwTile))
	y0 := int(math.Floor(y / bwTile))
	y1 := int(math.Floor((y + bwPH - 0.001) / bwTile))
	z0 := int(math.Floor(z / bwTile))
	z1 := int(math.Floor((z + bwPW - 0.001) / bwTile))
	for ty := y0; ty <= y1; ty++ {
		for tz := z0; tz <= z1; tz++ {
			for tx := x0; tx <= x1; tx++ {
				if a.solidTile3(tx, ty, tz) {
					return true
				}
			}
		}
	}
	return false
}

func (a *BlockWorldApp) stepPhysics3(dt float64) {
	forward, strafe := 0, 0
	if a.keys.forward {
		forward++
	}
	if a.keys.backward {
		forward--
	}
	if a.keys.right {
		strafe++
	}
	if a.keys.left {
		strafe--
	}
	if forward != 0 || strafe != 0 {
		length := math.Hypot(float64(forward), float64(strafe))
		f := float64(forward) / length
		s := float64(strafe) / length
		cy, sy := math.Cos(a.yaw), math.Sin(a.yaw)
		const speed = 112.0
		a.vx = (cy*f - sy*s) * speed
		a.vz = (sy*f + cy*s) * speed
		a.walk += dt * 11
		a.face = 1
		if s < 0 {
			a.face = -1
		}
	} else {
		a.vx, a.vz = 0, 0
		a.walk = 0
	}
	if a.vx != 0 {
		old := a.px
		a.px += a.vx * dt
		if a.collides3(a.px, a.py, a.pz) {
			a.px = old
			a.vx = 0
		}
	}
	if a.vz != 0 {
		old := a.pz
		a.pz += a.vz * dt
		if a.collides3(a.px, a.py, a.pz) {
			a.pz = old
			a.vz = 0
		}
	}
	a.vy += 900 * dt
	if a.vy > 640 {
		a.vy = 640
	}
	prevY := a.py
	a.py += a.vy * dt
	a.onGround = false
	if a.collides3(a.px, a.py, a.pz) {
		if a.vy > 0 {
			// Land on the highest solid top beneath the player's footprint.
			feet := prevY + bwPH
			top := feet
			x0 := int(math.Floor(a.px / bwTile))
			x1 := int(math.Floor((a.px + bwPW - 0.001) / bwTile))
			z0 := int(math.Floor(a.pz / bwTile))
			z1 := int(math.Floor((a.pz + bwPW - 0.001) / bwTile))
			y0 := int(math.Floor((feet - 0.001) / bwTile))
			for ty := y0; ty >= 0 && ty < a.world.H; ty-- {
				solid := false
				for tz := z0; tz <= z1 && !solid; tz++ {
					for tx := x0; tx <= x1; tx++ {
						if a.solidTile3(tx, ty, tz) {
							solid = true
							break
						}
					}
				}
				if solid {
					cand := float64(ty * bwTile)
					if cand <= feet+0.001 {
						top = cand
					}
					break
				}
			}
			a.py = top - bwPH
			for i := 0; i < 4 && a.collides3(a.px, a.py, a.pz); i++ {
				a.py -= 1
			}
			a.vy = 0
			a.onGround = true
		} else if a.vy < 0 {
			a.py = prevY
			for i := 0; i < 4 && a.collides3(a.px, a.py, a.pz); i++ {
				a.py += 1
			}
			a.vy = 0
		} else {
			a.py = prevY
		}
	}
	if a.py > float64(a.world.H*bwTile)+240 || a.px < -bwTile || a.px > float64(a.world.W*bwTile) ||
		a.pz < -bwTile || a.pz > float64(a.world.D*bwTile) {
		a.spawn()
		a.setStatus("你掉出了世界，已回到出生点")
	}
}

// raycast3 walks the voxel grid from the eye along the view direction. It
// returns both the first solid block and the empty cell just before it, which
// is where a newly placed block belongs.
func (a *BlockWorldApp) raycast3(maxDist float64) bwHit {
	ex, ey, ez := a.eye()
	dx, dy, dz := a.lookDir()
	const eps = 1e-6
	const inf = 1e30
	fx, fy, fz := ex/bwTile, ey/bwTile, ez/bwTile
	x, y, z := int(math.Floor(fx)), int(math.Floor(fy)), int(math.Floor(fz))
	stepX, stepY, stepZ := 0, 0, 0
	tMaxX, tMaxY, tMaxZ := inf, inf, inf
	tDeltaX, tDeltaY, tDeltaZ := inf, inf, inf
	if math.Abs(dx) > eps {
		if dx > 0 {
			stepX = 1
			tMaxX = (float64(x+1) - fx) / dx
		} else {
			stepX = -1
			tMaxX = (fx - float64(x)) / -dx
		}
		tDeltaX = 1 / math.Abs(dx)
	}
	if math.Abs(dy) > eps {
		if dy > 0 {
			stepY = 1
			tMaxY = (float64(y+1) - fy) / dy
		} else {
			stepY = -1
			tMaxY = (fy - float64(y)) / -dy
		}
		tDeltaY = 1 / math.Abs(dy)
	}
	if math.Abs(dz) > eps {
		if dz > 0 {
			stepZ = 1
			tMaxZ = (float64(z+1) - fz) / dz
		} else {
			stepZ = -1
			tMaxZ = (fz - float64(z)) / -dz
		}
		tDeltaZ = 1 / math.Abs(dz)
	}
	prevX, prevY, prevZ := x, y, z
	for i := 0; i < 256; i++ {
		if a.solidTile3(x, y, z) {
			if y < 0 || y >= a.world.H {
				return bwHit{}
			}
			return bwHit{ok: true, x: x, y: y, z: z,
				px: prevX, py: prevY, pz: prevZ,
				dist: math.Sqrt((float64(x)+0.5-fx)*(float64(x)+0.5-fx)*bwTile*bwTile +
					(float64(y)+0.5-fy)*(float64(y)+0.5-fy)*bwTile*bwTile +
					(float64(z)+0.5-fz)*(float64(z)+0.5-fz)*bwTile*bwTile)}
		}
		prevX, prevY, prevZ = x, y, z
		switch {
		case tMaxX <= tMaxY && tMaxX <= tMaxZ:
			if tMaxX > maxDist/bwTile {
				return bwHit{}
			}
			x += stepX
			tMaxX += tDeltaX
		case tMaxY <= tMaxZ:
			if tMaxY > maxDist/bwTile {
				return bwHit{}
			}
			y += stepY
			tMaxY += tDeltaY
		default:
			if tMaxZ > maxDist/bwTile {
				return bwHit{}
			}
			z += stepZ
			tMaxZ += tDeltaZ
		}
	}
	return bwHit{}
}

func (a *BlockWorldApp) updateHover3() {
	hit := a.raycast3(6.5 * bwTile)
	a.hover3Valid = hit.ok
	if hit.ok {
		a.hover3X, a.hover3Y, a.hover3Z = hit.x, hit.y, hit.z
		a.hover3PX, a.hover3PY, a.hover3PZ = hit.px, hit.py, hit.pz
	}
}

func (a *BlockWorldApp) withinReach3(tx, ty, tz int) bool {
	if tx < 0 || tx >= a.world.W || ty < 0 || ty >= a.world.H || tz < 0 || tz >= a.world.D {
		return false
	}
	ex, ey, ez := a.eye()
	cx := float64(tx*bwTile) + bwTile/2
	cy := float64(ty*bwTile) + bwTile/2
	cz := float64(tz*bwTile) + bwTile/2
	dx, dy, dz := cx-ex, cy-ey, cz-ez
	const reach = 7 * bwTile
	return dx*dx+dy*dy+dz*dz <= reach*reach
}

func (a *BlockWorldApp) stepMining3(dt float64) {
	if !a.mining || !a.hover3Valid {
		a.mineProg = 0
		return
	}
	tx, ty, tz := a.hover3X, a.hover3Y, a.hover3Z
	if tx != a.mine3X || ty != a.mine3Y || tz != a.mine3Z {
		a.mine3X, a.mine3Y, a.mine3Z = tx, ty, tz
		a.mineProg = 0
	}
	b := a.world.At3(tx, ty, tz)
	hard := 0.0
	if int(b) >= 0 && int(b) < len(bwHard) {
		hard = bwHard[b]
	}
	if b == bwAir || b == bwBedrock || hard < 0 {
		a.mineProg = 0
		return
	}
	a.mineProg += dt
	if a.mineProg >= hard {
		a.mineProg = 0
		a.breakBlock3(tx, ty, tz)
	}
}

func (a *BlockWorldApp) breakBlock3(tx, ty, tz int) bool {
	b := a.world.At3(tx, ty, tz)
	if b == bwAir || b == bwBedrock {
		return false
	}
	a.world.Set3(tx, ty, tz, bwAir)
	a.dirty = true
	drop := b
	if b == bwGrass {
		drop = bwDirt
	}
	a.addItem(drop)
	a.spawnParticles(tx, ty, b, 8)
	a.addFloat(tx, ty, "+1 "+bwNames[drop], 0xF0F0A0)
	bwSound("dig")
	if a.d != nil {
		a.d.Invalidate()
	}
	return true
}

func (a *BlockWorldApp) blockHitsPlayer3(tx, ty, tz int) bool {
	p := 0.02
	x0, x1 := float64(tx*bwTile)+p, float64((tx+1)*bwTile)-p
	y0, y1 := float64(ty*bwTile)+p, float64((ty+1)*bwTile)-p
	z0, z1 := float64(tz*bwTile)+p, float64((tz+1)*bwTile)-p
	return a.px < x1 && a.px+bwPW > x0 &&
		a.py < y1 && a.py+bwPH > y0 &&
		a.pz < z1 && a.pz+bwPW > z0
}

func (a *BlockWorldApp) placeBlock3(tx, ty, tz int) {
	if tx < 0 || tx >= a.world.W || ty < 1 || ty >= a.world.H-1 || tz < 0 || tz >= a.world.D {
		return
	}
	if !a.withinReach3(tx, ty, tz) {
		a.setStatus("距离太远，无法放置")
		return
	}
	if a.world.At3(tx, ty, tz) != bwAir {
		return
	}
	if a.blockHitsPlayer3(tx, ty, tz) {
		a.setStatus("不能把方块放在自己身上")
		return
	}
	slot := &a.hotbar[a.sel]
	if slot.Count <= 0 {
		a.setStatus("这个方块已经用完了")
		return
	}
	a.world.Set3(tx, ty, tz, slot.Block)
	slot.Count--
	if slot.Count == 0 {
		*slot = bwSlot{}
	}
	a.dirty = true
	bwSound("place")
	if a.d != nil {
		a.d.Invalidate()
	}
}

// render3D rasterizes the voxel world with one camera ray per screen sample.
// The buffer is rendered at a reduced resolution and upscaled with nearest
// neighbour sampling, which keeps the software path interactive on ARM.
func (a *BlockWorldApp) render3D(c *ui.Canvas, client ui.Rect) {
	if client.W < 8 || client.H < 8 {
		return
	}
	scale := 2
	if client.W*client.H > 320*240 {
		scale = 3
	}
	if client.W < 300 {
		scale = 1
	}
	bw := (client.W + scale - 1) / scale
	bh := (client.H + scale - 1) / scale
	if bw < 1 || bh < 1 {
		return
	}
	buf := make([]uint32, bw*bh)
	ex, ey, ez := a.eye()
	cp, sp := math.Cos(a.pitch), math.Sin(a.pitch)
	cy, sy := math.Cos(a.yaw), math.Sin(a.yaw)
	fwdX, fwdY, fwdZ := cy*cp, sp, sy*cp
	rightX, rightZ := sy, -cy
	upX, upY, upZ := cy*sp, -cp, sy*sp
	const fov = 1.0 // ~57 degrees half-angle
	aspect := float64(bw) / float64(bh)
	night := bwNightAmount(a.phase)
	skyTop, skyBot := bwSkyColors(a.phase)
	for py := 0; py < bh; py++ {
		v := (1 - 2*(float64(py)+0.5)/float64(bh)) * fov
		for px := 0; px < bw; px++ {
			u := (2*(float64(px)+0.5)/float64(bw) - 1) * fov * aspect
			dx := fwdX + rightX*u + upX*v
			dy := fwdY + upY*v
			dz := fwdZ + rightZ*u + upZ*v
			length := math.Sqrt(dx*dx + dy*dy + dz*dz)
			if length < 1e-6 {
				continue
			}
			dx, dy, dz = dx/length, dy/length, dz/length
			col, dist, hit := a.traceVoxel(ex, ey, ez, dx, dy, dz, 96*bwTile)
			if !hit {
				t := float64(py) / float64(bh)
				col = bwLerp(skyTop, skyBot, t)
				if night > 0.15 && py < bh/3 {
					h := bwHash(uint32(px/3), uint32(py/3))
					if h%17 == 0 {
						col = bwLerp(col, 0xFFFFFF, night*0.7)
					}
				}
			} else {
				fog := dist / (96 * bwTile)
				if fog > 0.85 {
					fog = 0.85
				}
				col = bwLerp(col, bwLerp(skyTop, skyBot, 0.45), fog*0.55)
			}
			buf[py*bw+px] = col
		}
	}
	// Blit with clipping and nearest-neighbour upscaling.
	x0 := max(client.X, 0)
	y0 := max(client.Y, 0)
	x1 := min(client.Right(), c.W)
	y1 := min(client.Bottom(), c.H)
	for y := y0; y < y1; y++ {
		syRow := (y - client.Y) / scale
		if syRow >= bh {
			syRow = bh - 1
		}
		row := syRow * bw
		drow := y * c.Stride
		for x := x0; x < x1; x++ {
			sxCol := (x - client.X) / scale
			if sxCol >= bw {
				sxCol = bw - 1
			}
			c.Pix[drow+x] = buf[row+sxCol]
		}
	}
}

// traceVoxel is the renderer's DDA loop with per-face shading. It is separate
// from raycast3 because it also needs the hit normal and distance.
func (a *BlockWorldApp) traceVoxel(ex, ey, ez, dx, dy, dz, maxDist float64) (uint32, float64, bool) {
	const eps = 1e-6
	const inf = 1e30
	fx, fy, fz := ex/bwTile, ey/bwTile, ez/bwTile
	x, y, z := int(math.Floor(fx)), int(math.Floor(fy)), int(math.Floor(fz))
	stepX, stepY, stepZ := 0, 0, 0
	tMaxX, tMaxY, tMaxZ := inf, inf, inf
	tDeltaX, tDeltaY, tDeltaZ := inf, inf, inf
	if math.Abs(dx) > eps {
		if dx > 0 {
			stepX = 1
			tMaxX = (float64(x+1) - fx) / dx
		} else {
			stepX = -1
			tMaxX = (fx - float64(x)) / -dx
		}
		tDeltaX = 1 / math.Abs(dx)
	}
	if math.Abs(dy) > eps {
		if dy > 0 {
			stepY = 1
			tMaxY = (float64(y+1) - fy) / dy
		} else {
			stepY = -1
			tMaxY = (fy - float64(y)) / -dy
		}
		tDeltaY = 1 / math.Abs(dy)
	}
	if math.Abs(dz) > eps {
		if dz > 0 {
			stepZ = 1
			tMaxZ = (float64(z+1) - fz) / dz
		} else {
			stepZ = -1
			tMaxZ = (fz - float64(z)) / -dz
		}
		tDeltaZ = 1 / math.Abs(dz)
	}
	normal := byte(0) // 0=x, 1=y, 2=z
	dist := 0.0
	for i := 0; i < 320; i++ {
		if a.solidTile3(x, y, z) {
			if x < 0 || x >= a.world.W || z < 0 || z >= a.world.D ||
				y < 0 || y >= a.world.H {
				return 0, 0, false
			}
			b := a.world.At3(x, y, z)
			shade := 1.0
			if normal == 1 {
				if dy > 0 {
					shade = 1.18
				} else {
					shade = 0.58
				}
			} else if normal == 0 {
				shade = 0.84
			} else {
				shade = 0.72
			}
			base := bwBaseColor[b]
			if b == bwGlass {
				base = 0xBCE8F8
			}
			h := bwHash(uint32(x*31+z*17), uint32(y*13)^uint32(a.world.Seed))
			speck := 0.94 + float64(h%13)/100
			col := bwShade(base, shade*speck*(0.62+0.38*(1-bwNightAmount(a.phase))))
			if b == bwLeaves {
				col = bwShade(col, 0.86+float64(h%7)/40)
			}
			return col, dist, true
		}
		switch {
		case tMaxX <= tMaxY && tMaxX <= tMaxZ:
			dist = tMaxX * bwTile
			if dist > maxDist {
				return 0, 0, false
			}
			x += stepX
			tMaxX += tDeltaX
			normal = 0
		case tMaxY <= tMaxZ:
			dist = tMaxY * bwTile
			if dist > maxDist {
				return 0, 0, false
			}
			y += stepY
			tMaxY += tDeltaY
			normal = 1
		default:
			dist = tMaxZ * bwTile
			if dist > maxDist {
				return 0, 0, false
			}
			z += stepZ
			tMaxZ += tDeltaZ
			normal = 2
		}
	}
	return 0, 0, false
}

func (a *BlockWorldApp) drawCrosshair3(c *ui.Canvas, client ui.Rect, f *ui.Font) {
	cx := client.X + client.W/2
	cy := client.Y + client.H/2
	for _, d := range [][2]int{{-1, 0}, {1, 0}, {0, -1}, {0, 1}} {
		for i := 2; i <= 8; i++ {
			c.Set(cx+d[0]*i, cy+d[1]*i, 0x000000)
		}
	}
	for i := -6; i <= 6; i++ {
		c.Set(cx+i, cy, 0xF0F0F0)
		c.Set(cx, cy+i, 0xF0F0F0)
	}
	if !a.hover3Valid {
		return
	}
	b := a.world.At3(a.hover3X, a.hover3Y, a.hover3Z)
	if b == bwAir {
		return
	}
	label := bwNames[b]
	lw := f.Width(label)
	lx := cx - lw/2
	ly := cy + 18
	c.FillRect(ui.Rect{lx - 5, ly - 3, lw + 10, 19}, 0x141C26)
	f.Text(c, lx, ly, label, 0xE8F0FF)
	if a.mining && a.mineProg > 0 {
		hard := bwHard[b]
		frac := 0.0
		if hard > 0 {
			frac = a.mineProg / hard
		}
		if frac > 1 {
			frac = 1
		}
		bar := ui.Rect{cx - 34, ly + 20, 68, 6}
		c.FillRect(bar, 0x000000)
		c.FillRect(ui.Rect{bar.X + 1, bar.Y + 1, int(float64(bar.W-2) * frac), bar.H - 2}, 0x60E080)
	}
}
