package core

import "github.com/spf13/viper"

// SetImageCacheMaxMB changes the image cache's size limit and saves it to the config file.
func (a *App) SetImageCacheMaxMB(mb int) error {
	if err := a.ImageCache.SetMaxMB(mb); err != nil {
		return err
	}
	viper.Set("cache.imageCacheMaxMB", mb)
	if err := viper.WriteConfig(); err != nil {
		a.Logger.Err(err).Msg("app: Failed to write config after setting the image cache limit")
	}
	return nil
}
