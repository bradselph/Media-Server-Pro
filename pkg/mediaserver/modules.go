package mediaserver

// ModuleID identifies a server module for selective registration.
type ModuleID string

// Module identifiers for use with WithModules.
const (
	ModDatabase      ModuleID = "database"
	ModSecurity      ModuleID = "security"
	ModAuth          ModuleID = "auth"
	ModMedia         ModuleID = "media"
	ModStreaming      ModuleID = "streaming"
	ModTasks         ModuleID = "tasks"
	ModScanner       ModuleID = "scanner"
	ModThumbnails    ModuleID = "thumbnails"
	ModHLS           ModuleID = "hls"
	ModAnalytics     ModuleID = "analytics"
	ModPlaylist      ModuleID = "playlist"
	ModAdmin         ModuleID = "admin"
	ModUpload        ModuleID = "upload"
	ModValidator     ModuleID = "validator"
	ModBackup        ModuleID = "backup"
	ModAutoDiscovery ModuleID = "autodiscovery"
	ModSuggestions   ModuleID = "suggestions"
	ModCategorizer   ModuleID = "categorizer"
	ModUpdater       ModuleID = "updater"
	ModRemote        ModuleID = "remote"
	ModDuplicates    ModuleID = "duplicates"
	ModReceiver      ModuleID = "receiver"
	ModDownloader    ModuleID = "downloader"
	ModExtractor     ModuleID = "extractor"
	ModCrawler       ModuleID = "crawler"
)

// ModuleSet is a named collection of modules for convenient selection.
type ModuleSet []ModuleID

// CoreModules includes only the modules required for basic media serving:
// database, auth, security, media library, streaming, thumbnails, and the task scheduler.
var CoreModules = ModuleSet{
	ModDatabase, ModSecurity, ModAuth, ModMedia,
	ModStreaming, ModTasks, ModScanner, ModThumbnails,
}

// StandardModules adds HLS transcoding, playlists, suggestions, analytics,
// uploads, and admin management on top of CoreModules.
var StandardModules = ModuleSet{
	ModDatabase, ModSecurity, ModAuth, ModMedia,
	ModStreaming, ModTasks, ModScanner, ModThumbnails,
	ModHLS, ModAnalytics, ModPlaylist, ModAdmin,
	ModUpload, ModSuggestions, ModCategorizer,
}

// AllModules includes every available module.
var AllModules = ModuleSet{
	ModDatabase, ModSecurity, ModAuth, ModMedia,
	ModStreaming, ModTasks, ModScanner, ModThumbnails,
	ModHLS, ModAnalytics, ModPlaylist, ModAdmin,
	ModUpload, ModValidator, ModBackup, ModAutoDiscovery,
	ModSuggestions, ModCategorizer, ModUpdater, ModRemote,
	ModDuplicates, ModReceiver, ModDownloader, ModExtractor,
	ModCrawler,
}

// moduleSetContains returns true if the set includes the given module ID.
func moduleSetContains(set ModuleSet, id ModuleID) bool {
	for _, m := range set {
		if m == id {
			return true
		}
	}
	return false
}
