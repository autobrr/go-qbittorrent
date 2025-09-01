package qbittorrent

import (
	"strconv"

	"github.com/autobrr/go-qbittorrent/errors"
)

var (
	ErrBadCredentials = errors.New("bad credentials")
	ErrIPBanned       = errors.New("User's IP is banned for too many failed login attempts")

	ErrUnexpectedStatus = errors.New("unexpected status code")

	ErrNoTorrentURLProvided           = errors.New("no torrent URL provided")
	ErrEmptySavePath                  = errors.New("save path is empty")
	ErrNoWriteAccessToPath            = errors.New("user does not have write access to directory")
	ErrCannotCreateSavePath           = errors.New("unable to create save path directory")
	ErrEmptyCategoryName              = errors.New("category name is empty")
	ErrInvalidCategoryName            = errors.New("category name is invalid")
	ErrCategoryEditingFailed          = errors.New("category editing failed")
	ErrCategoryDoesNotExist           = errors.New("category name does not exist")
	ErrInvalidPriority                = errors.New("priority is invalid or at least one id is not an integer")
	ErrTorrentNotFound                = errors.New("torrent not found")
	ErrTorrentMetdataNotDownloadedYet = errors.New("torrent metadata hasn't downloaded yet or at least one file id was not found")
	ErrMissingNewPathParameter        = errors.New("missing newPath parameter")
	ErrInvalidPathParameter           = errors.New("invalid newPath or oldPath, or newPath already in use")
	ErrInvalidTorrentHash             = errors.New("torrent hash is invalid")
	ErrEmptyTorrentName               = errors.New("torrent name is empty")
	ErrAllURLsNotFound                = errors.New("all urls were not found")
	ErrInvalidURL                     = errors.New("new url is not a valid URL")
	ErrTorrentQueueingNotEnabled      = errors.New("torrent queueing is not enabled, could not set hashes to max priority")
	ErrInvalidShareLimit              = errors.New("a share limit or at least one id is invalid")
	ErrInvalidCookies                 = errors.New("request was not a valid json array of cookie objects")
	ErrCannotGetTorrentPieceStates    = errors.New("could not get torrent piece states")
	ErrInvalidPeers                   = errors.New("none of the supplied peers are valid")

	ErrReannounceTookTooLong = errors.New("reannounce took too long, deleted torrent")
	ErrUnsupportedVersion    = errors.New("qBittorrent version too old, please upgrade to use this feature")
)

type Torrent struct {
	AddedOn            int64            `json:"added_on"`
	AmountLeft         int64            `json:"amount_left"`
	AutoManaged        bool             `json:"auto_tmm"`
	Availability       float64          `json:"availability"`
	Category           string           `json:"category"`
	Completed          int64            `json:"completed"`
	CompletionOn       int64            `json:"completion_on"`
	ContentPath        string           `json:"content_path"`
	DlLimit            int64            `json:"dl_limit"`
	DlSpeed            int64            `json:"dlspeed"`
	DownloadPath       string           `json:"download_path"`
	Downloaded         int64            `json:"downloaded"`
	DownloadedSession  int64            `json:"downloaded_session"`
	ETA                int64            `json:"eta"`
	FirstLastPiecePrio bool             `json:"f_l_piece_prio"`
	ForceStart         bool             `json:"force_start"`
	Hash               string           `json:"hash"`
	InfohashV1         string           `json:"infohash_v1"`
	InfohashV2         string           `json:"infohash_v2"`
	LastActivity       int64            `json:"last_activity"`
	MagnetURI          string           `json:"magnet_uri"`
	MaxRatio           float64          `json:"max_ratio"`
	MaxSeedingTime     int64            `json:"max_seeding_time"`
	Name               string           `json:"name"`
	NumComplete        int64            `json:"num_complete"`
	NumIncomplete      int64            `json:"num_incomplete"`
	NumLeechs          int64            `json:"num_leechs"`
	NumSeeds           int64            `json:"num_seeds"`
	Priority           int64            `json:"priority"`
	Progress           float64          `json:"progress"`
	Ratio              float64          `json:"ratio"`
	RatioLimit         float64          `json:"ratio_limit"`
	SavePath           string           `json:"save_path"`
	SeedingTime        int64            `json:"seeding_time"`
	SeedingTimeLimit   int64            `json:"seeding_time_limit"`
	SeenComplete       int64            `json:"seen_complete"`
	SequentialDownload bool             `json:"seq_dl"`
	Size               int64            `json:"size"`
	State              TorrentState     `json:"state"`
	SuperSeeding       bool             `json:"super_seeding"`
	Tags               string           `json:"tags"`
	TimeActive         int64            `json:"time_active"`
	TotalSize          int64            `json:"total_size"`
	Tracker            string           `json:"tracker"`
	TrackersCount      int64            `json:"trackers_count"`
	UpLimit            int64            `json:"up_limit"`
	Uploaded           int64            `json:"uploaded"`
	UploadedSession    int64            `json:"uploaded_session"`
	UpSpeed            int64            `json:"upspeed"`
	Trackers           []TorrentTracker `json:"trackers"`
}

type TorrentTrackersResponse struct {
	Trackers []TorrentTracker `json:"trackers"`
}

type TorrentTracker struct {
	// Tier          int   `json:"tier"` // can be both empty "" and int
	Url           string        `json:"url"`
	Status        TrackerStatus `json:"status"`
	NumPeers      int           `json:"num_peers"`
	NumSeeds      int           `json:"num_seeds"`
	NumLeechers   int           `json:"num_leechers"`
	NumDownloaded int           `json:"num_downloaded"`
	Message       string        `json:"msg"`
}

type TorrentFiles []struct {
	Availability float32 `json:"availability"`
	Index        int     `json:"index"`
	IsSeed       bool    `json:"is_seed,omitempty"`
	Name         string  `json:"name"`
	PieceRange   []int   `json:"piece_range"`
	Priority     int     `json:"priority"`
	Progress     float32 `json:"progress"`
	Size         int64   `json:"size"`
}

type Category struct {
	Name     string `json:"name"`
	SavePath string `json:"savePath"`
}

type TorrentState string

const (
	// Some error occurred, applies to paused torrents
	TorrentStateError TorrentState = "error"

	// Torrent data files is missing
	TorrentStateMissingFiles TorrentState = "missingFiles"

	// Torrent is being seeded and data is being transferred
	TorrentStateUploading TorrentState = "uploading"

	// Torrent is paused and has finished downloading
	TorrentStatePausedUp TorrentState = "pausedUP"

	// Torrent is stopped and has finished downloading
	TorrentStateStoppedUp TorrentState = "stoppedUP"

	// Queuing is enabled and torrent is queued for upload
	TorrentStateQueuedUp TorrentState = "queuedUP"

	// Torrent is being seeded, but no connection were made
	TorrentStateStalledUp TorrentState = "stalledUP"

	// Torrent has finished downloading and is being checked
	TorrentStateCheckingUp TorrentState = "checkingUP"

	// Torrent is forced to uploading and ignore queue limit
	TorrentStateForcedUp TorrentState = "forcedUP"

	// Torrent is allocating disk space for download
	TorrentStateAllocating TorrentState = "allocating"

	// Torrent is being downloaded and data is being transferred
	TorrentStateDownloading TorrentState = "downloading"

	// Torrent has just started downloading and is fetching metadata
	TorrentStateMetaDl TorrentState = "metaDL"

	// Torrent is paused and has NOT finished downloading
	TorrentStatePausedDl TorrentState = "pausedDL"

	// Torrent is stopped and has NOT finished downloading
	TorrentStateStoppedDl TorrentState = "stoppedDL"

	// Queuing is enabled and torrent is queued for download
	TorrentStateQueuedDl TorrentState = "queuedDL"

	// Torrent is being downloaded, but no connection were made
	TorrentStateStalledDl TorrentState = "stalledDL"

	// Same as checkingUP, but torrent has NOT finished downloading
	TorrentStateCheckingDl TorrentState = "checkingDL"

	// Torrent is forced to downloading to ignore queue limit
	TorrentStateForcedDl TorrentState = "forcedDL"

	// Checking resume data on qBt startup
	TorrentStateCheckingResumeData TorrentState = "checkingResumeData"

	// Torrent is moving to another location
	TorrentStateMoving TorrentState = "moving"

	// Unknown status
	TorrentStateUnknown TorrentState = "unknown"
)

type TorrentFilter string

const (
	// Torrent is paused
	TorrentFilterAll TorrentFilter = "all"

	// Torrent is active
	TorrentFilterActive TorrentFilter = "active"

	// Torrent is inactive
	TorrentFilterInactive TorrentFilter = "inactive"

	// Torrent is completed
	TorrentFilterCompleted TorrentFilter = "completed"

	// Torrent is resumed
	TorrentFilterResumed TorrentFilter = "resumed"

	// Torrent is paused
	TorrentFilterPaused TorrentFilter = "paused"

	// Torrent is stopped
	TorrentFilterStopped TorrentFilter = "stopped"

	// Torrent is stalled
	TorrentFilterStalled TorrentFilter = "stalled"

	// Torrent is being seeded and data is being transferred
	TorrentFilterUploading TorrentFilter = "uploading"

	// Torrent is being seeded, but no connection were made
	TorrentFilterStalledUploading TorrentFilter = "stalled_uploading"

	// Torrent is being downloaded and data is being transferred
	TorrentFilterDownloading TorrentFilter = "downloading"

	// Torrent is being downloaded, but no connection were made
	TorrentFilterStalledDownloading TorrentFilter = "stalled_downloading"

	// Torrent is errored
	TorrentFilterError TorrentFilter = "errored"
)

// TrackerStatus https://github.com/qbittorrent/qBittorrent/wiki/WebUI-API-(qBittorrent-4.1)#get-torrent-trackers
type TrackerStatus int

const (
	// 0 Tracker is disabled (used for DHT, PeX, and LSD)
	TrackerStatusDisabled TrackerStatus = 0

	// 1 Tracker has not been contacted yet
	TrackerStatusNotContacted TrackerStatus = 1

	// 2 Tracker has been contacted and is working
	TrackerStatusOK TrackerStatus = 2

	// 3 Tracker is updating
	TrackerStatusUpdating TrackerStatus = 3

	// 4 Tracker has been contacted, but it is not working (or doesn't send proper replies)
	TrackerStatusNotWorking TrackerStatus = 4
)

type ConnectionStatus string

const (
	ConnectionStatusConnected    = "connected"
	ConnectionStatusFirewalled   = "firewalled"
	ConnectionStatusDisconnected = "disconnected"
)

// TransferInfo
//
// https://github.com/qbittorrent/qBittorrent/wiki/WebUI-API-(qBittorrent-4.1)#get-global-transfer-info
//
// dl_info_speed 		integer 	Global download rate (bytes/s)
//
// dl_info_data 		integer 	Data downloaded this session (bytes)
//
// up_info_speed 		integer 	Global upload rate (bytes/s)
//
// up_info_data 		integer 	Data uploaded this session (bytes)
//
// dl_rate_limit 		integer 	Download rate limit (bytes/s)
//
// up_rate_limit 		integer 	Upload rate limit (bytes/s)
//
// dht_nodes 			integer 	DHT nodes connected to
//
// connection_status 	string 		Connection status. See possible values here below
type TransferInfo struct {
	ConnectionStatus ConnectionStatus `json:"connection_status"`
	DHTNodes         int64            `json:"dht_nodes"`
	DlInfoData       int64            `json:"dl_info_data"`
	DlInfoSpeed      int64            `json:"dl_info_speed"`
	DlRateLimit      int64            `json:"dl_rate_limit"`
	UpInfoData       int64            `json:"up_info_data"`
	UpInfoSpeed      int64            `json:"up_info_speed"`
	UpRateLimit      int64            `json:"up_rate_limit"`
}

type ContentLayout string

const (
	ContentLayoutOriginal        ContentLayout = "Original"
	ContentLayoutSubfolderNone   ContentLayout = "NoSubfolder"
	ContentLayoutSubfolderCreate ContentLayout = "Subfolder"
)

type TorrentAddOptions struct {
	Stopped            bool // introduced in Web API v2.11.0 (v5.0.0)
	Paused             bool
	SkipHashCheck      bool
	ContentLayout      ContentLayout
	SavePath           string
	DownloadPath       string
	UseDownloadPath    bool
	AutoTMM            bool
	Category           string
	Tags               string
	LimitUploadSpeed   int64
	LimitDownloadSpeed int64
	LimitRatio         float64
	LimitSeedTime      int64
	Rename             string
	FirstLastPiecePrio bool
	SequentialDownload bool
}

func (o *TorrentAddOptions) Prepare() map[string]string {
	options := map[string]string{}

	options["paused"] = "false"
	options["stopped"] = "false"
	if o.Paused {
		options["paused"] = "true"
		options["stopped"] = "true"
	}
	if o.Stopped {
		options["paused"] = "true"
		options["stopped"] = "true"
	}
	if o.SkipHashCheck {
		options["skip_checking"] = "true"
	}

	switch o.ContentLayout {
	case ContentLayoutSubfolderCreate:
		// pre qBittorrent version 4.3.2
		options["root_folder"] = "true"

		// post version 4.3.2
		options["contentLayout"] = string(ContentLayoutSubfolderCreate)
	case ContentLayoutSubfolderNone:
		// pre qBittorrent version 4.3.2
		options["root_folder"] = "false"

		// post version 4.3.2
		options["contentLayout"] = string(ContentLayoutSubfolderNone)
	}
	// if ORIGINAL then leave empty

	if o.SavePath != "" {
		options["savepath"] = o.SavePath
		options["autoTMM"] = "false"
	}
	if o.DownloadPath != "" {
		options["downloadPath"] = o.DownloadPath
		options["useDownloadPath"] = "true"
		options["autoTMM"] = "false"
	}
	if o.Category != "" {
		options["category"] = o.Category
	}
	if o.Tags != "" {
		options["tags"] = o.Tags
	}
	if o.LimitUploadSpeed > 0 {
		options["upLimit"] = strconv.FormatInt(o.LimitUploadSpeed*1024, 10)
	}
	if o.LimitDownloadSpeed > 0 {
		options["dlLimit"] = strconv.FormatInt(o.LimitDownloadSpeed*1024, 10)
	}
	if o.LimitRatio > 0 {
		options["ratioLimit"] = strconv.FormatFloat(o.LimitRatio, 'f', 2, 64)
	}
	if o.LimitSeedTime > 0 {
		options["seedingTimeLimit"] = strconv.FormatInt(o.LimitSeedTime, 10)
	}

	if o.Rename != "" {
		options["rename"] = o.Rename
	}

	options["firstLastPiecePrio"] = strconv.FormatBool(o.FirstLastPiecePrio)

	if o.SequentialDownload {
		options["sequentialDownload"] = "true"
	}

	return options
}

type TorrentFilterOptions struct {
	Filter          TorrentFilter
	Category        string
	Tag             string
	Sort            string
	Reverse         bool
	Limit           int
	Offset          int
	Hashes          []string
	IncludeTrackers bool // qbit 5.1+
}

type TorrentProperties struct {
	AdditionDate           int     `json:"addition_date"`
	Comment                string  `json:"comment"`
	CompletionDate         int     `json:"completion_date"`
	CreatedBy              string  `json:"created_by"`
	CreationDate           int     `json:"creation_date"`
	DlLimit                int     `json:"dl_limit"`
	DlSpeed                int     `json:"dl_speed"`
	DlSpeedAvg             int     `json:"dl_speed_avg"`
	DownloadPath           string  `json:"download_path"`
	Eta                    int     `json:"eta"`
	Hash                   string  `json:"hash"`
	InfohashV1             string  `json:"infohash_v1"`
	InfohashV2             string  `json:"infohash_v2"`
	IsPrivate              bool    `json:"is_private"`
	LastSeen               int     `json:"last_seen"`
	Name                   string  `json:"name"`
	NbConnections          int     `json:"nb_connections"`
	NbConnectionsLimit     int     `json:"nb_connections_limit"`
	Peers                  int     `json:"peers"`
	PeersTotal             int     `json:"peers_total"`
	PieceSize              int     `json:"piece_size"`
	PiecesHave             int     `json:"pieces_have"`
	PiecesNum              int     `json:"pieces_num"`
	Reannounce             int     `json:"reannounce"`
	SavePath               string  `json:"save_path"`
	SeedingTime            int     `json:"seeding_time"`
	Seeds                  int     `json:"seeds"`
	SeedsTotal             int     `json:"seeds_total"`
	ShareRatio             float64 `json:"share_ratio"`
	TimeElapsed            int     `json:"time_elapsed"`
	TotalDownloaded        int64   `json:"total_downloaded"`
	TotalDownloadedSession int64   `json:"total_downloaded_session"`
	TotalSize              int64   `json:"total_size"`
	TotalUploaded          int64   `json:"total_uploaded"`
	TotalUploadedSession   int64   `json:"total_uploaded_session"`
	TotalWasted            int64   `json:"total_wasted"`
	UpLimit                int     `json:"up_limit"`
	UpSpeed                int     `json:"up_speed"`
	UpSpeedAvg             int     `json:"up_speed_avg"`
}

type AppPreferences struct {
	AddTrackers                        string      `json:"add_trackers"`
	AddTrackersEnabled                 bool        `json:"add_trackers_enabled"`
	AltDlLimit                         int         `json:"alt_dl_limit"`
	AltUpLimit                         int         `json:"alt_up_limit"`
	AlternativeWebuiEnabled            bool        `json:"alternative_webui_enabled"`
	AlternativeWebuiPath               string      `json:"alternative_webui_path"`
	AnnounceIP                         string      `json:"announce_ip"`
	AnnounceToAllTiers                 bool        `json:"announce_to_all_tiers"`
	AnnounceToAllTrackers              bool        `json:"announce_to_all_trackers"`
	AnonymousMode                      bool        `json:"anonymous_mode"`
	AsyncIoThreads                     int         `json:"async_io_threads"`
	AutoDeleteMode                     int         `json:"auto_delete_mode"`
	AutoTmmEnabled                     bool        `json:"auto_tmm_enabled"`
	AutorunEnabled                     bool        `json:"autorun_enabled"`
	AutorunOnTorrentAddedEnabled       bool        `json:"autorun_on_torrent_added_enabled"`
	AutorunOnTorrentAddedProgram       string      `json:"autorun_on_torrent_added_program"`
	AutorunProgram                     string      `json:"autorun_program"`
	BannedIPs                          string      `json:"banned_IPs"`
	BittorrentProtocol                 int         `json:"bittorrent_protocol"`
	BlockPeersOnPrivilegedPorts        bool        `json:"block_peers_on_privileged_ports"`
	BypassAuthSubnetWhitelist          string      `json:"bypass_auth_subnet_whitelist"`
	BypassAuthSubnetWhitelistEnabled   bool        `json:"bypass_auth_subnet_whitelist_enabled"`
	BypassLocalAuth                    bool        `json:"bypass_local_auth"`
	CategoryChangedTmmEnabled          bool        `json:"category_changed_tmm_enabled"`
	CheckingMemoryUse                  int         `json:"checking_memory_use"`
	ConnectionSpeed                    int         `json:"connection_speed"`
	CurrentInterfaceAddress            string      `json:"current_interface_address"`
	CurrentNetworkInterface            string      `json:"current_network_interface"`
	Dht                                bool        `json:"dht"`
	DiskCache                          int         `json:"disk_cache"`
	DiskCacheTTL                       int         `json:"disk_cache_ttl"`
	DiskIoReadMode                     int         `json:"disk_io_read_mode"`
	DiskIoType                         int         `json:"disk_io_type"`
	DiskIoWriteMode                    int         `json:"disk_io_write_mode"`
	DiskQueueSize                      int         `json:"disk_queue_size"`
	DlLimit                            int         `json:"dl_limit"`
	DontCountSlowTorrents              bool        `json:"dont_count_slow_torrents"`
	DyndnsDomain                       string      `json:"dyndns_domain"`
	DyndnsEnabled                      bool        `json:"dyndns_enabled"`
	DyndnsPassword                     string      `json:"dyndns_password"`
	DyndnsService                      int         `json:"dyndns_service"`
	DyndnsUsername                     string      `json:"dyndns_username"`
	EmbeddedTrackerPort                int         `json:"embedded_tracker_port"`
	EmbeddedTrackerPortForwarding      bool        `json:"embedded_tracker_port_forwarding"`
	EnableCoalesceReadWrite            bool        `json:"enable_coalesce_read_write"`
	EnableEmbeddedTracker              bool        `json:"enable_embedded_tracker"`
	EnableMultiConnectionsFromSameIP   bool        `json:"enable_multi_connections_from_same_ip"`
	EnablePieceExtentAffinity          bool        `json:"enable_piece_extent_affinity"`
	EnableUploadSuggestions            bool        `json:"enable_upload_suggestions"`
	Encryption                         int         `json:"encryption"`
	ExcludedFileNames                  string      `json:"excluded_file_names"`
	ExcludedFileNamesEnabled           bool        `json:"excluded_file_names_enabled"`
	ExportDir                          string      `json:"export_dir"`
	ExportDirFin                       string      `json:"export_dir_fin"`
	FilePoolSize                       int         `json:"file_pool_size"`
	HashingThreads                     int         `json:"hashing_threads"`
	IdnSupportEnabled                  bool        `json:"idn_support_enabled"`
	IncompleteFilesExt                 bool        `json:"incomplete_files_ext"`
	IPFilterEnabled                    bool        `json:"ip_filter_enabled"`
	IPFilterPath                       string      `json:"ip_filter_path"`
	IPFilterTrackers                   bool        `json:"ip_filter_trackers"`
	LimitLanPeers                      bool        `json:"limit_lan_peers"`
	LimitTCPOverhead                   bool        `json:"limit_tcp_overhead"`
	LimitUtpRate                       bool        `json:"limit_utp_rate"`
	ListenPort                         int         `json:"listen_port"`
	Locale                             string      `json:"locale"`
	Lsd                                bool        `json:"lsd"`
	MailNotificationAuthEnabled        bool        `json:"mail_notification_auth_enabled"`
	MailNotificationEmail              string      `json:"mail_notification_email"`
	MailNotificationEnabled            bool        `json:"mail_notification_enabled"`
	MailNotificationPassword           string      `json:"mail_notification_password"`
	MailNotificationSender             string      `json:"mail_notification_sender"`
	MailNotificationSMTP               string      `json:"mail_notification_smtp"`
	MailNotificationSslEnabled         bool        `json:"mail_notification_ssl_enabled"`
	MailNotificationUsername           string      `json:"mail_notification_username"`
	MaxActiveCheckingTorrents          int         `json:"max_active_checking_torrents"`
	MaxActiveDownloads                 int         `json:"max_active_downloads"`
	MaxActiveTorrents                  int         `json:"max_active_torrents"`
	MaxActiveUploads                   int         `json:"max_active_uploads"`
	MaxConcurrentHTTPAnnounces         int         `json:"max_concurrent_http_announces"`
	MaxConnec                          int         `json:"max_connec"`
	MaxConnecPerTorrent                int         `json:"max_connec_per_torrent"`
	MaxRatio                           float64     `json:"max_ratio"`
	MaxRatioAct                        int         `json:"max_ratio_act"`
	MaxRatioEnabled                    bool        `json:"max_ratio_enabled"`
	MaxSeedingTime                     int         `json:"max_seeding_time"`
	MaxSeedingTimeEnabled              bool        `json:"max_seeding_time_enabled"`
	MaxUploads                         int         `json:"max_uploads"`
	MaxUploadsPerTorrent               int         `json:"max_uploads_per_torrent"`
	MemoryWorkingSetLimit              int         `json:"memory_working_set_limit"`
	OutgoingPortsMax                   int         `json:"outgoing_ports_max"`
	OutgoingPortsMin                   int         `json:"outgoing_ports_min"`
	PeerTos                            int         `json:"peer_tos"`
	PeerTurnover                       int         `json:"peer_turnover"`
	PeerTurnoverCutoff                 int         `json:"peer_turnover_cutoff"`
	PeerTurnoverInterval               int         `json:"peer_turnover_interval"`
	PerformanceWarning                 bool        `json:"performance_warning"`
	Pex                                bool        `json:"pex"`
	PreallocateAll                     bool        `json:"preallocate_all"`
	ProxyAuthEnabled                   bool        `json:"proxy_auth_enabled"`
	ProxyHostnameLookup                bool        `json:"proxy_hostname_lookup"`
	ProxyIP                            string      `json:"proxy_ip"`
	ProxyPassword                      string      `json:"proxy_password"`
	ProxyPeerConnections               bool        `json:"proxy_peer_connections"`
	ProxyPort                          int         `json:"proxy_port"`
	ProxyTorrentsOnly                  bool        `json:"proxy_torrents_only"`
	ProxyType                          interface{} `json:"proxy_type"` // pre 4.5.x this is an int and post 4.6.x it's a string
	ProxyUsername                      string      `json:"proxy_username"`
	QueueingEnabled                    bool        `json:"queueing_enabled"`
	RandomPort                         bool        `json:"random_port"`
	ReannounceWhenAddressChanged       bool        `json:"reannounce_when_address_changed"`
	RecheckCompletedTorrents           bool        `json:"recheck_completed_torrents"`
	RefreshInterval                    int         `json:"refresh_interval"`
	RequestQueueSize                   int         `json:"request_queue_size"`
	ResolvePeerCountries               bool        `json:"resolve_peer_countries"`
	ResumeDataStorageType              string      `json:"resume_data_storage_type"`
	RssAutoDownloadingEnabled          bool        `json:"rss_auto_downloading_enabled"`
	RssDownloadRepackProperEpisodes    bool        `json:"rss_download_repack_proper_episodes"`
	RssMaxArticlesPerFeed              int         `json:"rss_max_articles_per_feed"`
	RssProcessingEnabled               bool        `json:"rss_processing_enabled"`
	RssRefreshInterval                 int         `json:"rss_refresh_interval"`
	RssSmartEpisodeFilters             string      `json:"rss_smart_episode_filters"`
	SavePath                           string      `json:"save_path"`
	SavePathChangedTmmEnabled          bool        `json:"save_path_changed_tmm_enabled"`
	SaveResumeDataInterval             int         `json:"save_resume_data_interval"`
	ScanDirs                           struct{}    `json:"scan_dirs"`
	ScheduleFromHour                   int         `json:"schedule_from_hour"`
	ScheduleFromMin                    int         `json:"schedule_from_min"`
	ScheduleToHour                     int         `json:"schedule_to_hour"`
	ScheduleToMin                      int         `json:"schedule_to_min"`
	SchedulerDays                      int         `json:"scheduler_days"`
	SchedulerEnabled                   bool        `json:"scheduler_enabled"`
	SendBufferLowWatermark             int         `json:"send_buffer_low_watermark"`
	SendBufferWatermark                int         `json:"send_buffer_watermark"`
	SendBufferWatermarkFactor          int         `json:"send_buffer_watermark_factor"`
	SlowTorrentDlRateThreshold         int         `json:"slow_torrent_dl_rate_threshold"`
	SlowTorrentInactiveTimer           int         `json:"slow_torrent_inactive_timer"`
	SlowTorrentUlRateThreshold         int         `json:"slow_torrent_ul_rate_threshold"`
	SocketBacklogSize                  int         `json:"socket_backlog_size"`
	SsrfMitigation                     bool        `json:"ssrf_mitigation"`
	StartPausedEnabled                 bool        `json:"start_paused_enabled"`
	StopTrackerTimeout                 int         `json:"stop_tracker_timeout"`
	TempPath                           string      `json:"temp_path"`
	TempPathEnabled                    bool        `json:"temp_path_enabled"`
	TorrentChangedTmmEnabled           bool        `json:"torrent_changed_tmm_enabled"`
	TorrentContentLayout               string      `json:"torrent_content_layout"`
	TorrentStopCondition               string      `json:"torrent_stop_condition"`
	UpLimit                            int         `json:"up_limit"`
	UploadChokingAlgorithm             int         `json:"upload_choking_algorithm"`
	UploadSlotsBehavior                int         `json:"upload_slots_behavior"`
	Upnp                               bool        `json:"upnp"`
	UpnpLeaseDuration                  int         `json:"upnp_lease_duration"`
	UseCategoryPathsInManualMode       bool        `json:"use_category_paths_in_manual_mode"`
	UseHTTPS                           bool        `json:"use_https"`
	UtpTCPMixedMode                    int         `json:"utp_tcp_mixed_mode"`
	ValidateHTTPSTrackerCertificate    bool        `json:"validate_https_tracker_certificate"`
	WebUIAddress                       string      `json:"web_ui_address"`
	WebUIBanDuration                   int         `json:"web_ui_ban_duration"`
	WebUIClickjackingProtectionEnabled bool        `json:"web_ui_clickjacking_protection_enabled"`
	WebUICsrfProtectionEnabled         bool        `json:"web_ui_csrf_protection_enabled"`
	WebUICustomHTTPHeaders             string      `json:"web_ui_custom_http_headers"`
	WebUIDomainList                    string      `json:"web_ui_domain_list"`
	WebUIHostHeaderValidationEnabled   bool        `json:"web_ui_host_header_validation_enabled"`
	WebUIHTTPSCertPath                 string      `json:"web_ui_https_cert_path"`
	WebUIHTTPSKeyPath                  string      `json:"web_ui_https_key_path"`
	WebUIMaxAuthFailCount              int         `json:"web_ui_max_auth_fail_count"`
	WebUIPort                          int         `json:"web_ui_port"`
	WebUIReverseProxiesList            string      `json:"web_ui_reverse_proxies_list"`
	WebUIReverseProxyEnabled           bool        `json:"web_ui_reverse_proxy_enabled"`
	WebUISecureCookieEnabled           bool        `json:"web_ui_secure_cookie_enabled"`
	WebUISessionTimeout                int         `json:"web_ui_session_timeout"`
	WebUIUpnp                          bool        `json:"web_ui_upnp"`
	WebUIUseCustomHTTPHeadersEnabled   bool        `json:"web_ui_use_custom_http_headers_enabled"`
	WebUIUsername                      string      `json:"web_ui_username"`
}

type MainData struct {
	Rid               int64               `json:"rid"`
	FullUpdate        bool                `json:"full_update"`
	Torrents          map[string]Torrent  `json:"torrents"`
	TorrentsRemoved   []string            `json:"torrents_removed"`
	Categories        map[string]Category `json:"categories"`
	CategoriesRemoved []string            `json:"categories_removed"`
	Tags              []string            `json:"tags"`
	TagsRemoved       []string            `json:"tags_removed"`
	Trackers          map[string][]string `json:"trackers"`
	ServerState       ServerState         `json:"server_state"`
}

type ServerState struct {
	AlltimeDl            int64  `json:"alltime_dl"`
	AlltimeUl            int64  `json:"alltime_ul"`
	AverageTimeQueue     int64  `json:"average_time_queue"`
	ConnectionStatus     string `json:"connection_status"`
	DhtNodes             int64  `json:"dht_nodes"`
	DlInfoData           int64  `json:"dl_info_data"`
	DlInfoSpeed          int64  `json:"dl_info_speed"`
	DlRateLimit          int64  `json:"dl_rate_limit"`
	FreeSpaceOnDisk      int64  `json:"free_space_on_disk"`
	GlobalRatio          string `json:"global_ratio"`
	QueuedIoJobs         int64  `json:"queued_io_jobs"`
	Queueing             bool   `json:"queueing"`
	ReadCacheHits        string `json:"read_cache_hits"`
	ReadCacheOverload    string `json:"read_cache_overload"`
	RefreshInterval      int64  `json:"refresh_interval"`
	TotalBuffersSize     int64  `json:"total_buffers_size"`
	TotalPeerConnections int64  `json:"total_peer_connections"`
	TotalQueuedSize      int64  `json:"total_queued_size"`
	TotalWastedSession   int64  `json:"total_wasted_session"`
	UpInfoData           int64  `json:"up_info_data"`
	UpInfoSpeed          int64  `json:"up_info_speed"`
	UpRateLimit          int64  `json:"up_rate_limit"`
	UseAltSpeedLimits    bool   `json:"use_alt_speed_limits"`
	WriteCacheOverload   string `json:"write_cache_overload"`
}

// Log
type Log struct {
	ID        int64  `json:"id"`
	Message   string `json:"message"`
	Timestamp int64  `json:"timestamp"`
	Type      int64  `json:"type"`
}

// PeerLog
type PeerLog struct {
	ID        int64  `json:"id"`
	IP        string `json:"ip"`
	Blocked   bool   `json:"blocked"`
	Timestamp int64  `json:"timestamp"`
	Reason    string `json:"reason"`
}

type BuildInfo struct {
	Qt         string `json:"qt"`         // QT version
	Libtorrent string `json:"libtorrent"` // libtorrent version
	Boost      string `json:"boost"`      // Boost version
	Openssl    string `json:"openssl"`    // OpenSSL version
	Bitness    int    `json:"bitness"`    // Application bitness (e.g.64-bit)
}

type Cookie struct {
	Name           string `json:"name"`           // Cookie name
	Domain         string `json:"domain"`         // Cookie domain
	Path           string `json:"path"`           // Cookie path
	Value          string `json:"value"`          // Cookie value
	ExpirationDate int64  `json:"expirationDate"` // Seconds since epoch
}

// PieceState represents download state of torrent pieces.
type PieceState int

const (
	PieceStateNotDownloadYet    = 0
	PieceStateNowDownloading    = 1
	PieceStateAlreadyDownloaded = 2
)

// silence unused variable warnings
var (
	_ = PieceStateNotDownloadYet
	_ = PieceStateNowDownloading
	_ = PieceStateAlreadyDownloaded
)

type WebSeed struct {
	URL string `json:"url"`
}
