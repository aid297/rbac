package config

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/spf13/viper"
)

func (c *Config) normalizeAdmin() {
	if c == nil {
		return
	}
	if strings.TrimSpace(c.Admin.Username) == "" {
		c.Admin.Username = DefaultAdminUser
	}
	if c.Admin.Password == "" {
		c.Admin.Password = DefaultAdminPassword
	}
	if strings.TrimSpace(c.Admin.Host) == "" {
		c.Admin.Host = DefaultAdminHost
	}
	if c.Admin.Port == 0 {
		c.Admin.Port = DefaultAdminPort
	}
}

// ReadAdmin loads only the admin section. Empty username/password become admin/admin.
func ReadAdmin(path string) (Admin, error) {
	v := viper.New()
	v.SetConfigFile(path)
	v.SetConfigType("yaml")
	if err := v.ReadInConfig(); err != nil {
		return Admin{}, fmt.Errorf("config: read admin %s: %w", path, err)
	}
	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return Admin{}, fmt.Errorf("config: unmarshal admin: %w", err)
	}
	cfg.normalizeAdmin()
	return cfg.Admin, nil
}

// WatchFile calls onChange after the file is created, written, or replaced.
func WatchFile(path string, stop <-chan struct{}, onChange func()) error {
	path = absPath(path)
	dir := filepath.Dir(path)
	base := filepath.Base(path)
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("config: watch: %w", err)
	}
	if err := w.Add(dir); err != nil {
		_ = w.Close()
		return fmt.Errorf("config: watch %s: %w", dir, err)
	}
	go func() {
		defer w.Close()
		var timer *time.Timer
		fire := func() {
			if onChange != nil {
				onChange()
			}
		}
		for {
			select {
			case <-stop:
				return
			case _, ok := <-w.Errors:
				if !ok {
					return
				}
			case ev, ok := <-w.Events:
				if !ok {
					return
				}
				if filepath.Base(ev.Name) != base {
					continue
				}
				if ev.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Rename) == 0 {
					continue
				}
				if timer == nil {
					timer = time.AfterFunc(120*time.Millisecond, fire)
				} else {
					timer.Reset(120 * time.Millisecond)
				}
			}
		}
	}()
	return nil
}
