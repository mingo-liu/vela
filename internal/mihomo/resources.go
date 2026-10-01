package mihomo

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/mingo-liu/vela/internal/profile"
)

func (r *Runner) compile(raw []byte, apiPort int, tun bool) ([]byte, error) {
	rules, err := r.store.CustomRules()
	if err != nil {
		return nil, err
	}
	return r.compileWithCustomRules(raw, apiPort, r.secret, tun, rules)
}

func (r *Runner) compileWithCustomRules(raw []byte, apiPort int, secret string, tun bool, rules []profile.CustomRule) ([]byte, error) {
	settings, err := r.store.Settings()
	if err != nil {
		return nil, err
	}
	raw, err = profile.ApplyCustomRules(raw, rules)
	if err != nil {
		return nil, err
	}
	return profile.CompileWithLogLevel(raw, r.state.Port, apiPort, secret, tun, r.state.RoutingMode, settings.LogLevel)
}

func (r *Runner) ensureGeoIPDatabase(profile []byte) error {
	upper := bytes.ToUpper(profile)
	if bytes.Contains(upper, []byte("GEOIP,")) {
		found := false
		for _, name := range []string{"Country.mmdb", "geoip.db", "geoip.metadb"} {
			if _, err := os.Stat(filepath.Join(r.dataDir, name)); err == nil {
				found = true
				break
			} else if !errors.Is(err, os.ErrNotExist) {
				return err
			}
		}
		if !found {
			if err := r.copyGeoResource("Country.mmdb", "缺少 GeoIP 数据库，请重新打包应用"); err != nil {
				return err
			}
		}
	}
	if bytes.Contains(upper, []byte("GEOSITE,")) {
		if _, err := os.Stat(filepath.Join(r.dataDir, "geosite.dat")); errors.Is(err, os.ErrNotExist) {
			return r.copyGeoResource("geosite.dat", "缺少 GeoSite 数据库，请重新打包应用")
		} else if err != nil {
			return err
		}
	}
	return nil
}

func (r *Runner) copyGeoResource(name, missing string) error {
	data, err := os.ReadFile(filepath.Join(filepath.Dir(r.binary), name))
	if errors.Is(err, os.ErrNotExist) {
		return errors.New(missing)
	}
	if err != nil {
		return fmt.Errorf("读取 %s 数据库失败: %w", name, err)
	}
	return writePrivate(filepath.Join(r.dataDir, name), data)
}

func writePrivate(path string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".runtime-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err := f.Chmod(0600); err != nil {
		f.Close()
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
