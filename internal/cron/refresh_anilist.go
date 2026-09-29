package cron

import (
	"context"
	"seanime/internal/api/anilist"
	"seanime/internal/events"
)

func RefreshAnilistDataJob(c *JobCtx) {
	defer func() {
		if r := recover(); r != nil {
		}
	}()

	if c.App.Settings == nil || c.App.Settings.Library == nil {
		return
	}

	// A scheduled refresh only uses the AniList budget that people browsing can spare.
	ctx := anilist.WithBackgroundPriority(context.Background())

	// Refresh the Anilist Collection
	animeCollection, _ := c.App.RefreshAnimeCollection(ctx)
	c.App.WSEventManager.SendEvent(events.RefreshedAnilistAnimeCollection, animeCollection)

	mangaCollection, _ := c.App.RefreshMangaCollection(ctx)
	c.App.WSEventManager.SendEvent(events.RefreshedAnilistMangaCollection, mangaCollection)
}

func SyncLocalDataJob(c *JobCtx) {
	defer func() {
		if r := recover(); r != nil {
		}
	}()

	if c.App.Settings == nil || c.App.Settings.Library == nil {
		return
	}

	// Only synchronize local data if the user is not simulated
	if c.App.Settings.Library.AutoSyncOfflineLocalData && !c.App.GetUser().IsSimulated {
		c.App.LocalManager.SynchronizeLocal()
	}

	// Only synchronize local data if the user is not simulated
	if c.App.Settings.Library.AutoSaveCurrentMediaOffline && !c.App.GetUser().IsSimulated {
		added, _ := c.App.LocalManager.AutoTrackCurrentMedia()
		if added && c.App.Settings.Library.AutoSyncOfflineLocalData {
			go c.App.LocalManager.SynchronizeLocal()
		}
	}
}
