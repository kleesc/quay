package database

import (
	"database/sql/driver"
	"encoding/json"
	"time"
)

// JSON represents a JSON field that can be stored in the database
type JSON map[string]interface{}

// Value implements the driver.Valuer interface for JSON fields
func (j JSON) Value() (driver.Value, error) {
	if j == nil {
		return nil, nil
	}
	return json.Marshal(j)
}

// Scan implements the sql.Scanner interface for JSON fields
func (j *JSON) Scan(value interface{}) error {
	if value == nil {
		*j = nil
		return nil
	}
	
	bytes, ok := value.([]byte)
	if !ok {
		return nil
	}
	
	return json.Unmarshal(bytes, j)
}

// User represents a user, organization, or robot account
type User struct {
	ID                        int        `json:"id" db:"id"`
	UUID                      *string    `json:"uuid" db:"uuid"`
	Username                  string     `json:"username" db:"username"`
	PasswordHash              *string    `json:"-" db:"password_hash"`
	Email                     string     `json:"email" db:"email"`
	Verified                  bool       `json:"verified" db:"verified"`
	StripeID                  *string    `json:"stripe_id" db:"stripe_id"`
	Organization              bool       `json:"organization" db:"organization"`
	Robot                     bool       `json:"robot" db:"robot"`
	InvoiceEmail              bool       `json:"invoice_email" db:"invoice_email"`
	InvalidLoginAttempts      int        `json:"invalid_login_attempts" db:"invalid_login_attempts"`
	LastInvalidLogin          time.Time  `json:"last_invalid_login" db:"last_invalid_login"`
	RemovedTagExpirationS     int64      `json:"removed_tag_expiration_s" db:"removed_tag_expiration_s"`
	Enabled                   bool       `json:"enabled" db:"enabled"`
	InvoiceEmailAddress       *string    `json:"invoice_email_address" db:"invoice_email_address"`
	GivenName                 *string    `json:"given_name" db:"given_name"`
	FamilyName                *string    `json:"family_name" db:"family_name"`
	Company                   *string    `json:"company" db:"company"`
	Location                  *string    `json:"location" db:"location"`
	MaximumQueuedBuildsCount  *int       `json:"maximum_queued_builds_count" db:"maximum_queued_builds_count"`
	CreationDate              *time.Time `json:"creation_date" db:"creation_date"`
	LastAccessed              *time.Time `json:"last_accessed" db:"last_accessed"`
}

// RobotAccountMetadata represents metadata for robot accounts
type RobotAccountMetadata struct {
	ID                int    `json:"id" db:"id"`
	RobotAccountID    int    `json:"robot_account_id" db:"robot_account_id"`
	Description       string `json:"description" db:"description"`
	UnstructuredJSON  JSON   `json:"unstructured_json" db:"unstructured_json"`
}

// RobotAccountToken represents tokens for robot accounts
type RobotAccountToken struct {
	ID               int    `json:"id" db:"id"`
	RobotAccountID   int    `json:"robot_account_id" db:"robot_account_id"`
	Token            string `json:"token" db:"token"`
	FullyMigrated    bool   `json:"fully_migrated" db:"fully_migrated"`
}

// QuotaType represents quota types
type QuotaType struct {
	ID   int    `json:"id" db:"id"`
	Name string `json:"name" db:"name"`
}

// UserOrganizationQuota represents organization quotas
type UserOrganizationQuota struct {
	ID           int   `json:"id" db:"id"`
	NamespaceID  int   `json:"namespace_id" db:"namespace_id"`
	LimitBytes   int64 `json:"limit_bytes" db:"limit_bytes"`
}

// QuotaLimits represents quota limits
type QuotaLimits struct {
	ID               int `json:"id" db:"id"`
	QuotaID          int `json:"quota_id" db:"quota_id"`
	QuotaTypeID      int `json:"quota_type_id" db:"quota_type_id"`
	PercentOfLimit   int `json:"percent_of_limit" db:"percent_of_limit"`
}

// DeletedNamespace represents deleted namespaces
type DeletedNamespace struct {
	ID               int       `json:"id" db:"id"`
	NamespaceID      int       `json:"namespace_id" db:"namespace_id"`
	Marked           time.Time `json:"marked" db:"marked"`
	OriginalUsername string    `json:"original_username" db:"original_username"`
	OriginalEmail    string    `json:"original_email" db:"original_email"`
	QueueID          *string   `json:"queue_id" db:"queue_id"`
}

// DeletedRepository represents deleted repositories
type DeletedRepository struct {
	ID                int       `json:"id" db:"id"`
	RepositoryID      int       `json:"repository_id" db:"repository_id"`
	Marked            time.Time `json:"marked" db:"marked"`
	OriginalName      string    `json:"original_name" db:"original_name"`
	OriginalNamespace string    `json:"original_namespace" db:"original_namespace"`
	QueueID           *string   `json:"queue_id" db:"queue_id"`
}

// NamespaceGeoRestriction represents geo restrictions for namespaces
type NamespaceGeoRestriction struct {
	ID                        int       `json:"id" db:"id"`
	NamespaceID               int       `json:"namespace_id" db:"namespace_id"`
	Added                     time.Time `json:"added" db:"added"`
	Description               string    `json:"description" db:"description"`
	UnstructuredJSON          JSON      `json:"unstructured_json" db:"unstructured_json"`
	RestrictedRegionISOCode   string    `json:"restricted_region_iso_code" db:"restricted_region_iso_code"`
}

// UserPromptKind represents user prompt kinds
type UserPromptKind struct {
	ID   int    `json:"id" db:"id"`
	Name string `json:"name" db:"name"`
}

// UserPrompt represents user prompts
type UserPrompt struct {
	ID     int `json:"id" db:"id"`
	UserID int `json:"user_id" db:"user_id"`
	KindID int `json:"kind_id" db:"kind_id"`
}

// TeamRole represents team roles
type TeamRole struct {
	ID   int    `json:"id" db:"id"`
	Name string `json:"name" db:"name"`
}

// Team represents organization teams
type Team struct {
	ID             int    `json:"id" db:"id"`
	Name           string `json:"name" db:"name"`
	OrganizationID int    `json:"organization_id" db:"organization_id"`
	RoleID         int    `json:"role_id" db:"role_id"`
	Description    string `json:"description" db:"description"`
}

// TeamMember represents team membership
type TeamMember struct {
	ID     int `json:"id" db:"id"`
	UserID int `json:"user_id" db:"user_id"`
	TeamID int `json:"team_id" db:"team_id"`
}

// TeamMemberInvite represents team member invitations
type TeamMemberInvite struct {
	ID          int     `json:"id" db:"id"`
	UserID      *int    `json:"user_id" db:"user_id"`
	Email       *string `json:"email" db:"email"`
	TeamID      int     `json:"team_id" db:"team_id"`
	InviterID   int     `json:"inviter_id" db:"inviter_id"`
	InviteToken string  `json:"invite_token" db:"invite_token"`
}

// LoginService represents login services
type LoginService struct {
	ID   int    `json:"id" db:"id"`
	Name string `json:"name" db:"name"`
}

// TeamSync represents team synchronization
type TeamSync struct {
	ID             int        `json:"id" db:"id"`
	TeamID         int        `json:"team_id" db:"team_id"`
	TransactionID  string     `json:"transaction_id" db:"transaction_id"`
	LastUpdated    *time.Time `json:"last_updated" db:"last_updated"`
	ServiceID      int        `json:"service_id" db:"service_id"`
	Config         JSON       `json:"config" db:"config"`
}

// FederatedLogin represents federated login
type FederatedLogin struct {
	ID            int    `json:"id" db:"id"`
	UserID        int    `json:"user_id" db:"user_id"`
	ServiceID     int    `json:"service_id" db:"service_id"`
	ServiceIdent  string `json:"service_ident" db:"service_ident"`
	MetadataJSON  string `json:"metadata_json" db:"metadata_json"`
}

// Visibility represents repository visibility
type Visibility struct {
	ID   int    `json:"id" db:"id"`
	Name string `json:"name" db:"name"`
}

// RepositoryKind represents repository kinds
type RepositoryKind struct {
	ID   int    `json:"id" db:"id"`
	Name string `json:"name" db:"name"`
}

// Repository represents a container repository
type Repository struct {
	ID              int    `json:"id" db:"id"`
	NamespaceUserID *int   `json:"namespace_user_id" db:"namespace_user_id"`
	Name            string `json:"name" db:"name"`
	VisibilityID    int    `json:"visibility_id" db:"visibility_id"`
	Description     *string `json:"description" db:"description"`
	BadgeToken      string `json:"badge_token" db:"badge_token"`
	KindID          int    `json:"kind_id" db:"kind_id"`
	TrustEnabled    bool   `json:"trust_enabled" db:"trust_enabled"`
	State           int    `json:"state" db:"state"`
}

// RepositorySearchScore represents repository search scores
type RepositorySearchScore struct {
	ID           int        `json:"id" db:"id"`
	RepositoryID int        `json:"repository_id" db:"repository_id"`
	Score        int64      `json:"score" db:"score"`
	LastUpdated  *time.Time `json:"last_updated" db:"last_updated"`
}

// QuotaNamespaceSize represents namespace quota sizes
type QuotaNamespaceSize struct {
	ID                 int   `json:"id" db:"id"`
	NamespaceUserID    int   `json:"namespace_user_id" db:"namespace_user_id"`
	SizeBytes          int64 `json:"size_bytes" db:"size_bytes"`
	BackfillStartMS    *int64 `json:"backfill_start_ms" db:"backfill_start_ms"`
	BackfillComplete   bool  `json:"backfill_complete" db:"backfill_complete"`
}

// QuotaRepositorySize represents repository quota sizes
type QuotaRepositorySize struct {
	ID                 int   `json:"id" db:"id"`
	RepositoryID       int   `json:"repository_id" db:"repository_id"`
	SizeBytes          int64 `json:"size_bytes" db:"size_bytes"`
	BackfillStartMS    *int64 `json:"backfill_start_ms" db:"backfill_start_ms"`
	BackfillComplete   bool  `json:"backfill_complete" db:"backfill_complete"`
}

// QuotaRegistrySize represents registry quota sizes
type QuotaRegistrySize struct {
	ID           int   `json:"id" db:"id"`
	SizeBytes    int64 `json:"size_bytes" db:"size_bytes"`
	Running      bool  `json:"running" db:"running"`
	Queued       bool  `json:"queued" db:"queued"`
	CompletedMS  *int64 `json:"completed_ms" db:"completed_ms"`
}

// Star represents repository stars
type Star struct {
	ID           int       `json:"id" db:"id"`
	UserID       int       `json:"user_id" db:"user_id"`
	RepositoryID int       `json:"repository_id" db:"repository_id"`
	Created      time.Time `json:"created" db:"created"`
}

// Role represents permission roles
type Role struct {
	ID   int    `json:"id" db:"id"`
	Name string `json:"name" db:"name"`
}

// RepositoryPermission represents repository permissions
type RepositoryPermission struct {
	ID           int  `json:"id" db:"id"`
	TeamID       *int `json:"team_id" db:"team_id"`
	UserID       *int `json:"user_id" db:"user_id"`
	RepositoryID int  `json:"repository_id" db:"repository_id"`
	RoleID       int  `json:"role_id" db:"role_id"`
}

// PermissionPrototype represents permission prototypes
type PermissionPrototype struct {
	ID               int     `json:"id" db:"id"`
	OrgID            int     `json:"org_id" db:"org_id"`
	UUID             string  `json:"uuid" db:"uuid"`
	ActivatingUserID *int    `json:"activating_user_id" db:"activating_user_id"`
	DelegateUserID   *int    `json:"delegate_user_id" db:"delegate_user_id"`
	DelegateTeamID   *int    `json:"delegate_team_id" db:"delegate_team_id"`
	RoleID           int     `json:"role_id" db:"role_id"`
}

// AccessTokenKind represents access token kinds
type AccessTokenKind struct {
	ID   int    `json:"id" db:"id"`
	Name string `json:"name" db:"name"`
}

// AccessToken represents access tokens
type AccessToken struct {
	ID           int        `json:"id" db:"id"`
	FriendlyName *string    `json:"friendly_name" db:"friendly_name"`
	TokenName    string     `json:"token_name" db:"token_name"`
	TokenCode    string     `json:"token_code" db:"token_code"`
	RepositoryID int        `json:"repository_id" db:"repository_id"`
	Created      time.Time  `json:"created" db:"created"`
	RoleID       int        `json:"role_id" db:"role_id"`
	Temporary    bool       `json:"temporary" db:"temporary"`
	KindID       *int       `json:"kind_id" db:"kind_id"`
}

// BuildTriggerService represents build trigger services
type BuildTriggerService struct {
	ID   int    `json:"id" db:"id"`
	Name string `json:"name" db:"name"`
}

// DisableReason represents disable reasons
type DisableReason struct {
	ID   int    `json:"id" db:"id"`
	Name string `json:"name" db:"name"`
}

// RepositoryBuildTrigger represents repository build triggers
type RepositoryBuildTrigger struct {
	ID                          int        `json:"id" db:"id"`
	UUID                        string     `json:"uuid" db:"uuid"`
	ServiceID                   int        `json:"service_id" db:"service_id"`
	RepositoryID                int        `json:"repository_id" db:"repository_id"`
	ConnectedUserID             int        `json:"connected_user_id" db:"connected_user_id"`
	SecureAuthToken             *string    `json:"secure_auth_token" db:"secure_auth_token"`
	SecurePrivateKey            *string    `json:"secure_private_key" db:"secure_private_key"`
	FullyMigrated               bool       `json:"fully_migrated" db:"fully_migrated"`
	Config                      string     `json:"config" db:"config"`
	WriteTokenID                *int       `json:"write_token_id" db:"write_token_id"`
	PullRobotID                 *int       `json:"pull_robot_id" db:"pull_robot_id"`
	Enabled                     bool       `json:"enabled" db:"enabled"`
	DisabledReasonID            *int       `json:"disabled_reason_id" db:"disabled_reason_id"`
	DisabledDatetime            *time.Time `json:"disabled_datetime" db:"disabled_datetime"`
	SuccessiveFailureCount      int        `json:"successive_failure_count" db:"successive_failure_count"`
	SuccessiveInternalErrorCount int       `json:"successive_internal_error_count" db:"successive_internal_error_count"`
}

// EmailConfirmation represents email confirmations
type EmailConfirmation struct {
	ID               int       `json:"id" db:"id"`
	Code             string    `json:"code" db:"code"`
	VerificationCode *string   `json:"verification_code" db:"verification_code"`
	UserID           int       `json:"user_id" db:"user_id"`
	PwReset          bool      `json:"pw_reset" db:"pw_reset"`
	NewEmail         *string   `json:"new_email" db:"new_email"`
	EmailConfirm     bool      `json:"email_confirm" db:"email_confirm"`
	Created          time.Time `json:"created" db:"created"`
}

// ImageStorage represents blob storage
type ImageStorage struct {
	ID               int     `json:"id" db:"id"`
	UUID             string  `json:"uuid" db:"uuid"`
	ImageSize        *int64  `json:"image_size" db:"image_size"`
	UncompressedSize *int64  `json:"uncompressed_size" db:"uncompressed_size"`
	Uploading        *bool   `json:"uploading" db:"uploading"`
	CASPath          bool    `json:"cas_path" db:"cas_path"`
	ContentChecksum  *string `json:"content_checksum" db:"content_checksum"`
}

// ImageStorageTransformation represents image storage transformations
type ImageStorageTransformation struct {
	ID   int    `json:"id" db:"id"`
	Name string `json:"name" db:"name"`
}

// ImageStorageSignatureKind represents image storage signature kinds
type ImageStorageSignatureKind struct {
	ID   int    `json:"id" db:"id"`
	Name string `json:"name" db:"name"`
}

// ImageStorageSignature represents image storage signatures
type ImageStorageSignature struct {
	ID        int     `json:"id" db:"id"`
	StorageID int     `json:"storage_id" db:"storage_id"`
	KindID    int     `json:"kind_id" db:"kind_id"`
	Signature *string `json:"signature" db:"signature"`
	Uploading *bool   `json:"uploading" db:"uploading"`
}

// ImageStorageLocation represents image storage locations
type ImageStorageLocation struct {
	ID   int    `json:"id" db:"id"`
	Name string `json:"name" db:"name"`
}

// ImageStoragePlacement represents image storage placements
type ImageStoragePlacement struct {
	ID         int `json:"id" db:"id"`
	StorageID  int `json:"storage_id" db:"storage_id"`
	LocationID int `json:"location_id" db:"location_id"`
}

// UserRegion represents user regions
type UserRegion struct {
	ID         int `json:"id" db:"id"`
	UserID     int `json:"user_id" db:"user_id"`
	LocationID int `json:"location_id" db:"location_id"`
}

// QueueItem represents queue items
type QueueItem struct {
	ID                 int       `json:"id" db:"id"`
	QueueName          string    `json:"queue_name" db:"queue_name"`
	Body               string    `json:"body" db:"body"`
	AvailableAfter     time.Time `json:"available_after" db:"available_after"`
	Available          bool      `json:"available" db:"available"`
	ProcessingExpires  *time.Time `json:"processing_expires" db:"processing_expires"`
	RetriesRemaining   int       `json:"retries_remaining" db:"retries_remaining"`
	StateID            string    `json:"state_id" db:"state_id"`
}

// RepositoryBuild represents repository builds
type RepositoryBuild struct {
	ID            int       `json:"id" db:"id"`
	UUID          string    `json:"uuid" db:"uuid"`
	RepositoryID  int       `json:"repository_id" db:"repository_id"`
	AccessTokenID int       `json:"access_token_id" db:"access_token_id"`
	ResourceKey   *string   `json:"resource_key" db:"resource_key"`
	JobConfig     string    `json:"job_config" db:"job_config"`
	Phase         string    `json:"phase" db:"phase"`
	Started       time.Time `json:"started" db:"started"`
	DisplayName   string    `json:"display_name" db:"display_name"`
	TriggerID     *int      `json:"trigger_id" db:"trigger_id"`
	PullRobotID   *int      `json:"pull_robot_id" db:"pull_robot_id"`
	LogsArchived  bool      `json:"logs_archived" db:"logs_archived"`
	QueueID       *string   `json:"queue_id" db:"queue_id"`
}

// LogEntryKind represents log entry kinds
type LogEntryKind struct {
	ID   int    `json:"id" db:"id"`
	Name string `json:"name" db:"name"`
}

// LogEntry represents log entries
type LogEntry struct {
	ID           int64     `json:"id" db:"id"`
	KindID       int       `json:"kind_id" db:"kind_id"`
	AccountID    int       `json:"account_id" db:"account_id"`
	PerformerID  *int      `json:"performer_id" db:"performer_id"`
	RepositoryID *int      `json:"repository_id" db:"repository_id"`
	DateTime     time.Time `json:"datetime" db:"datetime"`
	IP           *string   `json:"ip" db:"ip"`
	MetadataJSON string    `json:"metadata_json" db:"metadata_json"`
}

// LogEntry2 represents log entries (temp for quay.io)
type LogEntry2 struct {
	ID           int       `json:"id" db:"id"`
	KindID       int       `json:"kind_id" db:"kind_id"`
	AccountID    int       `json:"account_id" db:"account_id"`
	PerformerID  *int      `json:"performer_id" db:"performer_id"`
	RepositoryID *int      `json:"repository_id" db:"repository_id"`
	DateTime     time.Time `json:"datetime" db:"datetime"`
	IP           *string   `json:"ip" db:"ip"`
	MetadataJSON string    `json:"metadata_json" db:"metadata_json"`
}

// LogEntry3 represents log entries (version 3)
type LogEntry3 struct {
	ID           int64     `json:"id" db:"id"`
	KindID       int       `json:"kind_id" db:"kind_id"`
	AccountID    int       `json:"account_id" db:"account_id"`
	PerformerID  *int      `json:"performer_id" db:"performer_id"`
	RepositoryID *int      `json:"repository_id" db:"repository_id"`
	DateTime     time.Time `json:"datetime" db:"datetime"`
	IP           *string   `json:"ip" db:"ip"`
	MetadataJSON string    `json:"metadata_json" db:"metadata_json"`
}

// RepositoryActionCount represents repository action counts
type RepositoryActionCount struct {
	ID           int       `json:"id" db:"id"`
	RepositoryID int       `json:"repository_id" db:"repository_id"`
	Count        int       `json:"count" db:"count"`
	Date         time.Time `json:"date" db:"date"`
}

// OAuthApplication represents OAuth applications
type OAuthApplication struct {
	ID                    int     `json:"id" db:"id"`
	ClientID              string  `json:"client_id" db:"client_id"`
	SecureClientSecret    *string `json:"secure_client_secret" db:"secure_client_secret"`
	FullyMigrated         bool    `json:"fully_migrated" db:"fully_migrated"`
	RedirectURI           string  `json:"redirect_uri" db:"redirect_uri"`
	ApplicationURI        string  `json:"application_uri" db:"application_uri"`
	OrganizationID        int     `json:"organization_id" db:"organization_id"`
	Name                  string  `json:"name" db:"name"`
	Description           string  `json:"description" db:"description"`
	AvatarEmail           *string `json:"avatar_email" db:"avatar_email"`
}

// OAuthAuthorizationCode represents OAuth authorization codes
type OAuthAuthorizationCode struct {
	ID              int    `json:"id" db:"id"`
	ApplicationID   int    `json:"application_id" db:"application_id"`
	CodeName        string `json:"code_name" db:"code_name"`
	CodeCredential  string `json:"code_credential" db:"code_credential"`
	Scope           string `json:"scope" db:"scope"`
	Data            string `json:"data" db:"data"`
}

// OAuthAccessToken represents OAuth access tokens
type OAuthAccessToken struct {
	ID               int       `json:"id" db:"id"`
	UUID             string    `json:"uuid" db:"uuid"`
	ApplicationID    int       `json:"application_id" db:"application_id"`
	AuthorizedUserID int       `json:"authorized_user_id" db:"authorized_user_id"`
	Scope            string    `json:"scope" db:"scope"`
	TokenName        string    `json:"token_name" db:"token_name"`
	TokenCode        string    `json:"token_code" db:"token_code"`
	TokenType        string    `json:"token_type" db:"token_type"`
	ExpiresAt        time.Time `json:"expires_at" db:"expires_at"`
	Data             string    `json:"data" db:"data"`
}

// NotificationKind represents notification kinds
type NotificationKind struct {
	ID   int    `json:"id" db:"id"`
	Name string `json:"name" db:"name"`
}

// Notification represents notifications
type Notification struct {
	ID           int       `json:"id" db:"id"`
	UUID         string    `json:"uuid" db:"uuid"`
	KindID       int       `json:"kind_id" db:"kind_id"`
	TargetID     int       `json:"target_id" db:"target_id"`
	MetadataJSON string    `json:"metadata_json" db:"metadata_json"`
	Created      time.Time `json:"created" db:"created"`
	Dismissed    bool      `json:"dismissed" db:"dismissed"`
	LookupPath   *string   `json:"lookup_path" db:"lookup_path"`
}

// ExternalNotificationEvent represents external notification events
type ExternalNotificationEvent struct {
	ID   int    `json:"id" db:"id"`
	Name string `json:"name" db:"name"`
}

// ExternalNotificationMethod represents external notification methods
type ExternalNotificationMethod struct {
	ID   int    `json:"id" db:"id"`
	Name string `json:"name" db:"name"`
}

// RepositoryNotification represents repository notifications
type RepositoryNotification struct {
	ID                int     `json:"id" db:"id"`
	UUID              string  `json:"uuid" db:"uuid"`
	RepositoryID      int     `json:"repository_id" db:"repository_id"`
	EventID           int     `json:"event_id" db:"event_id"`
	MethodID          int     `json:"method_id" db:"method_id"`
	Title             *string `json:"title" db:"title"`
	ConfigJSON        string  `json:"config_json" db:"config_json"`
	EventConfigJSON   string  `json:"event_config_json" db:"event_config_json"`
	NumberOfFailures  int     `json:"number_of_failures" db:"number_of_failures"`
	LastRanMS         *int64  `json:"last_ran_ms" db:"last_ran_ms"`
}

// RepositoryAuthorizedEmail represents repository authorized emails
type RepositoryAuthorizedEmail struct {
	ID           int    `json:"id" db:"id"`
	RepositoryID int    `json:"repository_id" db:"repository_id"`
	Email        string `json:"email" db:"email"`
	Code         string `json:"code" db:"code"`
	Confirmed    bool   `json:"confirmed" db:"confirmed"`
}

// UploadedBlob represents uploaded blobs
type UploadedBlob struct {
	ID                    int64     `json:"id" db:"id"`
	RepositoryID          int       `json:"repository_id" db:"repository_id"`
	BlobID                *int      `json:"blob_id" db:"blob_id"`
	ByteCount             *int64    `json:"byte_count" db:"byte_count"`
	UncompressedByteCount *int64    `json:"uncompressed_byte_count" db:"uncompressed_byte_count"`
	UploadState           *string   `json:"upload_state" db:"upload_state"`
	SHAState              *string   `json:"sha_state" db:"sha_state"`
	PieceHashes           JSON      `json:"piece_hashes" db:"piece_hashes"`
	StorageMetadata       JSON      `json:"storage_metadata" db:"storage_metadata"`
	UUID                  string    `json:"uuid" db:"uuid"`
	LocationID            int       `json:"location_id" db:"location_id"`
	ExpiresAt             time.Time `json:"expires_at" db:"expires_at"`
	Created               time.Time `json:"created" db:"created"`
}

// BlobUpload represents blob uploads
type BlobUpload struct {
	ID                     int       `json:"id" db:"id"`
	RepositoryID           int       `json:"repository_id" db:"repository_id"`
	UUID                   string    `json:"uuid" db:"uuid"`
	ByteCount              int64     `json:"byte_count" db:"byte_count"`
	ShaState               *string   `json:"sha_state" db:"sha_state"`
	LocationID             int       `json:"location_id" db:"location_id"`
	StorageMetadata        JSON      `json:"storage_metadata" db:"storage_metadata"`
	ChunkCount             int       `json:"chunk_count" db:"chunk_count"`
	UncompressedByteCount  *int64    `json:"uncompressed_byte_count" db:"uncompressed_byte_count"`
	Created                time.Time `json:"created" db:"created"`
	PieceShaState          *string   `json:"piece_sha_state" db:"piece_sha_state"`
	PieceHashes            *string   `json:"piece_hashes" db:"piece_hashes"`
}

// QuayService represents Quay services
type QuayService struct {
	ID   int    `json:"id" db:"id"`
	Name string `json:"name" db:"name"`
}

// QuayRegion represents Quay regions
type QuayRegion struct {
	ID   int    `json:"id" db:"id"`
	Name string `json:"name" db:"name"`
}

// QuayRelease represents Quay releases
type QuayRelease struct {
	ID        int       `json:"id" db:"id"`
	ServiceID int       `json:"service_id" db:"service_id"`
	Version   string    `json:"version" db:"version"`
	RegionID  int       `json:"region_id" db:"region_id"`
	Reverted  bool      `json:"reverted" db:"reverted"`
	Created   time.Time `json:"created" db:"created"`
}

// ServiceKeyApproval represents service key approvals
type ServiceKeyApproval struct {
	ID           int       `json:"id" db:"id"`
	ApproverID   *int      `json:"approver_id" db:"approver_id"`
	ApprovalType string    `json:"approval_type" db:"approval_type"`
	ApprovedDate time.Time `json:"approved_date" db:"approved_date"`
	Notes        string    `json:"notes" db:"notes"`
}

// ServiceKey represents service keys
type ServiceKey struct {
	ID               int        `json:"id" db:"id"`
	Name             string     `json:"name" db:"name"`
	Kid              string     `json:"kid" db:"kid"`
	Service          string     `json:"service" db:"service"`
	JWK              JSON       `json:"jwk" db:"jwk"`
	Metadata         JSON       `json:"metadata" db:"metadata"`
	CreatedDate      time.Time  `json:"created_date" db:"created_date"`
	ExpirationDate   *time.Time `json:"expiration_date" db:"expiration_date"`
	RotationDuration *int       `json:"rotation_duration" db:"rotation_duration"`
	ApprovalID       *int       `json:"approval_id" db:"approval_id"`
}

// MediaType represents media types
type MediaType struct {
	ID   int    `json:"id" db:"id"`
	Name string `json:"name" db:"name"`
}

// Messages represents messages
type Messages struct {
	ID          int    `json:"id" db:"id"`
	Content     string `json:"content" db:"content"`
	UUID        string `json:"uuid" db:"uuid"`
	Severity    string `json:"severity" db:"severity"`
	MediaTypeID int    `json:"media_type_id" db:"media_type_id"`
}

// LabelSourceType represents label source types
type LabelSourceType struct {
	ID      int    `json:"id" db:"id"`
	Name    string `json:"name" db:"name"`
	Mutable bool   `json:"mutable" db:"mutable"`
}

// Label represents labels
type Label struct {
	ID             int    `json:"id" db:"id"`
	UUID           string `json:"uuid" db:"uuid"`
	Key            string `json:"key" db:"key"`
	Value          string `json:"value" db:"value"`
	MediaTypeID    int    `json:"media_type_id" db:"media_type_id"`
	SourceTypeID   int    `json:"source_type_id" db:"source_type_id"`
}

// ApprBlob represents App Registry blobs
type ApprBlob struct {
	ID               int    `json:"id" db:"id"`
	Digest           string `json:"digest" db:"digest"`
	MediaTypeID      int    `json:"media_type_id" db:"media_type_id"`
	Size             int64  `json:"size" db:"size"`
	UncompressedSize *int64 `json:"uncompressed_size" db:"uncompressed_size"`
}

// ApprBlobPlacementLocation represents App Registry blob placement locations
type ApprBlobPlacementLocation struct {
	ID   int    `json:"id" db:"id"`
	Name string `json:"name" db:"name"`
}

// ApprBlobPlacement represents App Registry blob placements
type ApprBlobPlacement struct {
	ID         int `json:"id" db:"id"`
	BlobID     int `json:"blob_id" db:"blob_id"`
	LocationID int `json:"location_id" db:"location_id"`
}

// ApprManifest represents App Registry manifests
type ApprManifest struct {
	ID           int    `json:"id" db:"id"`
	Digest       string `json:"digest" db:"digest"`
	MediaTypeID  int    `json:"media_type_id" db:"media_type_id"`
	ManifestJSON JSON   `json:"manifest_json" db:"manifest_json"`
}

// ApprManifestBlob represents App Registry manifest blobs
type ApprManifestBlob struct {
	ID         int `json:"id" db:"id"`
	ManifestID int `json:"manifest_id" db:"manifest_id"`
	BlobID     int `json:"blob_id" db:"blob_id"`
}

// ApprManifestList represents App Registry manifest lists
type ApprManifestList struct {
	ID               int    `json:"id" db:"id"`
	Digest           string `json:"digest" db:"digest"`
	ManifestListJSON JSON   `json:"manifest_list_json" db:"manifest_list_json"`
	SchemaVersion    string `json:"schema_version" db:"schema_version"`
	MediaTypeID      int    `json:"media_type_id" db:"media_type_id"`
}

// ApprTagKind represents App Registry tag kinds
type ApprTagKind struct {
	ID   int    `json:"id" db:"id"`
	Name string `json:"name" db:"name"`
}

// ApprTag represents App Registry tags
type ApprTag struct {
	ID             int     `json:"id" db:"id"`
	Name           string  `json:"name" db:"name"`
	RepositoryID   int     `json:"repository_id" db:"repository_id"`
	ManifestListID *int    `json:"manifest_list_id" db:"manifest_list_id"`
	LifetimeStart  int64   `json:"lifetime_start" db:"lifetime_start"`
	LifetimeEnd    *int64  `json:"lifetime_end" db:"lifetime_end"`
	Hidden         bool    `json:"hidden" db:"hidden"`
	Reverted       bool    `json:"reverted" db:"reverted"`
	Protected      bool    `json:"protected" db:"protected"`
	TagKindID      int     `json:"tag_kind_id" db:"tag_kind_id"`
	LinkedTagID    *int    `json:"linked_tag_id" db:"linked_tag_id"`
}

// ApprManifestListManifest represents App Registry manifest list manifests
type ApprManifestListManifest struct {
	ID               int     `json:"id" db:"id"`
	ManifestListID   int     `json:"manifest_list_id" db:"manifest_list_id"`
	ManifestID       int     `json:"manifest_id" db:"manifest_id"`
	OperatingSystem  *string `json:"operating_system" db:"operating_system"`
	Architecture     *string `json:"architecture" db:"architecture"`
	PlatformJSON     JSON    `json:"platform_json" db:"platform_json"`
	MediaTypeID      int     `json:"media_type_id" db:"media_type_id"`
}

// AppSpecificAuthToken represents application-specific authentication tokens
type AppSpecificAuthToken struct {
	ID           int        `json:"id" db:"id"`
	UserID       int        `json:"user_id" db:"user_id"`
	UUID         string     `json:"uuid" db:"uuid"`
	Title        string     `json:"title" db:"title"`
	TokenName    string     `json:"token_name" db:"token_name"`
	TokenSecret  string     `json:"token_secret" db:"token_secret"`
	Created      time.Time  `json:"created" db:"created"`
	Expiration   *time.Time `json:"expiration" db:"expiration"`
	LastAccessed *time.Time `json:"last_accessed" db:"last_accessed"`
}

// Manifest represents container image manifests
type Manifest struct {
	ID                     int     `json:"id" db:"id"`
	RepositoryID           int     `json:"repository_id" db:"repository_id"`
	Digest                 string  `json:"digest" db:"digest"`
	MediaTypeID            int     `json:"media_type_id" db:"media_type_id"`
	ManifestBytes          string  `json:"manifest_bytes" db:"manifest_bytes"`
	ConfigMediaType        *string `json:"config_media_type" db:"config_media_type"`
	LayersCompressedSize   *int64  `json:"layers_compressed_size" db:"layers_compressed_size"`
	Subject                *string `json:"subject" db:"subject"`
	SubjectBackfilled      bool    `json:"subject_backfilled" db:"subject_backfilled"`
	ArtifactType           *string `json:"artifact_type" db:"artifact_type"`
	ArtifactTypeBackfilled bool    `json:"artifact_type_backfilled" db:"artifact_type_backfilled"`
}

// TagKind represents tag kinds
type TagKind struct {
	ID   int    `json:"id" db:"id"`
	Name string `json:"name" db:"name"`
}

// Tag represents repository tags
type Tag struct {
	ID              int     `json:"id" db:"id"`
	Name            string  `json:"name" db:"name"`
	RepositoryID    int     `json:"repository_id" db:"repository_id"`
	ManifestID      *int    `json:"manifest_id" db:"manifest_id"`
	LifetimeStartMS int64   `json:"lifetime_start_ms" db:"lifetime_start_ms"`
	LifetimeEndMS   *int64  `json:"lifetime_end_ms" db:"lifetime_end_ms"`
	Immutable       bool    `json:"immutable" db:"immutable"`
	Hidden          bool    `json:"hidden" db:"hidden"`
	Reversion       bool    `json:"reversion" db:"reversion"`
	TagKindID       int     `json:"tag_kind_id" db:"tag_kind_id"`
	LinkedTagID     *int    `json:"linked_tag_id" db:"linked_tag_id"`
}

// ManifestChild represents manifest child relationships
type ManifestChild struct {
	ID              int `json:"id" db:"id"`
	RepositoryID    int `json:"repository_id" db:"repository_id"`
	ManifestID      int `json:"manifest_id" db:"manifest_id"`
	ChildManifestID int `json:"child_manifest_id" db:"child_manifest_id"`
}

// ManifestLabel represents manifest labels
type ManifestLabel struct {
	ID           int `json:"id" db:"id"`
	RepositoryID int `json:"repository_id" db:"repository_id"`
	ManifestID   int `json:"manifest_id" db:"manifest_id"`
	LabelID      int `json:"label_id" db:"label_id"`
}

// ManifestBlob represents manifest blobs
type ManifestBlob struct {
	ID           int `json:"id" db:"id"`
	RepositoryID int `json:"repository_id" db:"repository_id"`
	ManifestID   int `json:"manifest_id" db:"manifest_id"`
	BlobID       int `json:"blob_id" db:"blob_id"`
}

// RepoMirrorRule represents repository mirror rules
type RepoMirrorRule struct {
	ID           int       `json:"id" db:"id"`
	UUID         string    `json:"uuid" db:"uuid"`
	RepositoryID int       `json:"repository_id" db:"repository_id"`
	CreationDate time.Time `json:"creation_date" db:"creation_date"`
	RuleType     int       `json:"rule_type" db:"rule_type"`
	RuleValue    JSON      `json:"rule_value" db:"rule_value"`
	LeftChildID  *int      `json:"left_child_id" db:"left_child_id"`
	RightChildID *int      `json:"right_child_id" db:"right_child_id"`
}

// RepoMirrorConfig represents repository mirror configuration
type RepoMirrorConfig struct {
	ID                        int       `json:"id" db:"id"`
	RepositoryID              int       `json:"repository_id" db:"repository_id"`
	CreationDate              time.Time `json:"creation_date" db:"creation_date"`
	IsEnabled                 bool      `json:"is_enabled" db:"is_enabled"`
	MirrorType                int       `json:"mirror_type" db:"mirror_type"`
	InternalRobotID           int       `json:"internal_robot_id" db:"internal_robot_id"`
	ExternalReference         string    `json:"external_reference" db:"external_reference"`
	ExternalRegistryUsername  *string   `json:"external_registry_username" db:"external_registry_username"`
	ExternalRegistryPassword  *string   `json:"external_registry_password" db:"external_registry_password"`
	ExternalRegistryConfig    JSON      `json:"external_registry_config" db:"external_registry_config"`
	SyncInterval              int       `json:"sync_interval" db:"sync_interval"`
	SyncStartDate             *time.Time `json:"sync_start_date" db:"sync_start_date"`
	SyncExpirationDate        *time.Time `json:"sync_expiration_date" db:"sync_expiration_date"`
	SyncRetriesRemaining      int       `json:"sync_retries_remaining" db:"sync_retries_remaining"`
	SyncStatus                int       `json:"sync_status" db:"sync_status"`
	SyncTransactionID         string    `json:"sync_transaction_id" db:"sync_transaction_id"`
	RootRuleID                int       `json:"root_rule_id" db:"root_rule_id"`
	SkopeoTimeout             int64     `json:"skopeo_timeout" db:"skopeo_timeout"`
}

// ManifestSecurityStatus represents manifest security status
type ManifestSecurityStatus struct {
	ID              int       `json:"id" db:"id"`
	ManifestID      int       `json:"manifest_id" db:"manifest_id"`
	RepositoryID    int       `json:"repository_id" db:"repository_id"`
	IndexStatus     int       `json:"index_status" db:"index_status"`
	ErrorJSON       JSON      `json:"error_json" db:"error_json"`
	LastIndexed     time.Time `json:"last_indexed" db:"last_indexed"`
	IndexerHash     string    `json:"indexer_hash" db:"indexer_hash"`
	IndexerVersion  int       `json:"indexer_version" db:"indexer_version"`
	MetadataJSON    JSON      `json:"metadata_json" db:"metadata_json"`
}

// ProxyCacheConfig represents proxy cache configuration
type ProxyCacheConfig struct {
	ID                       int     `json:"id" db:"id"`
	OrganizationID           int     `json:"organization_id" db:"organization_id"`
	CreationDate             time.Time `json:"creation_date" db:"creation_date"`
	UpstreamRegistry         string  `json:"upstream_registry" db:"upstream_registry"`
	UpstreamRegistryUsername *string `json:"upstream_registry_username" db:"upstream_registry_username"`
	UpstreamRegistryPassword *string `json:"upstream_registry_password" db:"upstream_registry_password"`
	ExpirationS              int     `json:"expiration_s" db:"expiration_s"`
	Insecure                 bool    `json:"insecure" db:"insecure"`
}

// RedHatSubscriptions represents Red Hat subscriptions
type RedHatSubscriptions struct {
	ID            int `json:"id" db:"id"`
	UserID        int `json:"user_id" db:"user_id"`
	AccountNumber int `json:"account_number" db:"account_number"`
}

// OrganizationRhSkus represents organization Red Hat SKUs
type OrganizationRhSkus struct {
	ID             int  `json:"id" db:"id"`
	SubscriptionID int  `json:"subscription_id" db:"subscription_id"`
	UserID         int  `json:"user_id" db:"user_id"`
	OrgID          int  `json:"org_id" db:"org_id"`
	Quantity       *int `json:"quantity" db:"quantity"`
}

// NamespaceAutoPrunePolicy represents namespace auto-prune policies
type NamespaceAutoPrunePolicy struct {
	ID          int    `json:"id" db:"id"`
	UUID        string `json:"uuid" db:"uuid"`
	NamespaceID int    `json:"namespace_id" db:"namespace_id"`
	Policy      JSON   `json:"policy" db:"policy"`
}

// AutoPruneTaskStatus represents auto-prune task status
type AutoPruneTaskStatus struct {
	ID          int     `json:"id" db:"id"`
	NamespaceID int     `json:"namespace_id" db:"namespace_id"`
	LastRanMS   *int64  `json:"last_ran_ms" db:"last_ran_ms"`
	Status      *string `json:"status" db:"status"`
}

// RepositoryAutoPrunePolicy represents repository auto-prune policies
type RepositoryAutoPrunePolicy struct {
	ID           int    `json:"id" db:"id"`
	UUID         string `json:"uuid" db:"uuid"`
	RepositoryID int    `json:"repository_id" db:"repository_id"`
	NamespaceID  int    `json:"namespace_id" db:"namespace_id"`
	Policy       JSON   `json:"policy" db:"policy"`
}

// OauthAssignedToken represents OAuth assigned tokens
type OauthAssignedToken struct {
	ID             int     `json:"id" db:"id"`
	UUID           string  `json:"uuid" db:"uuid"`
	AssignedUserID int     `json:"assigned_user_id" db:"assigned_user_id"`
	ApplicationID  int     `json:"application_id" db:"application_id"`
	RedirectURI    *string `json:"redirect_uri" db:"redirect_uri"`
	Scope          string  `json:"scope" db:"scope"`
	ResponseType   *string `json:"response_type" db:"response_type"`
}

// TagNotificationSuccess represents tag notification success
type TagNotificationSuccess struct {
	ID             int `json:"id" db:"id"`
	NotificationID int `json:"notification_id" db:"notification_id"`
	TagID          int `json:"tag_id" db:"tag_id"`
	MethodID       int `json:"method_id" db:"method_id"`
}

// Enums and constants
const (
	// Repository States
	RepositoryStateNormal            = 0
	RepositoryStateReadOnly          = 1
	RepositoryStateMirror            = 2
	RepositoryStateMarkedForDeletion = 3

	// Build Phases
	BuildPhaseError          = "error"
	BuildPhaseInternalError  = "internalerror"
	BuildPhaseBuildScheduled = "build-scheduled"
	BuildPhaseUnpacking      = "unpacking"
	BuildPhasePulling        = "pulling"
	BuildPhaseBuilding       = "building"
	BuildPhasePushing        = "pushing"
	BuildPhaseWaiting        = "waiting"
	BuildPhaseComplete       = "complete"
	BuildPhaseCancelled      = "cancelled"

	// Repo Mirror Rule Types
	RepoMirrorRuleTypeTagGlobCSV = 1

	// Repo Mirror Types
	RepoMirrorTypePull = 1

	// Repo Mirror Status
	RepoMirrorStatusCancel    = -2
	RepoMirrorStatusFail      = -1
	RepoMirrorStatusNeverRun  = 0
	RepoMirrorStatusSuccess   = 1
	RepoMirrorStatusSyncing   = 2
	RepoMirrorStatusSyncNow   = 3

	// Index Status
	IndexStatusManifestLayerTooLarge = -3
	IndexStatusManifestUnsupported   = -2
	IndexStatusFailed                = -1
	IndexStatusInProgress            = 1
	IndexStatusCompleted             = 2

	// Indexer Versions
	IndexerVersionV2 = 2
	IndexerVersionV4 = 4

	// Default Proxy Cache Expiration
	DefaultProxyCacheExpiration = 86400 // 24 hours in seconds

	// Image not scanned engine version
	ImageNotScannedEngineVersion = -1

	// Quota Types
	QuotaTypeWarning = "Warning"
	QuotaTypeReject  = "Reject"

	// User Prompt Types
	UserPromptTypeConfirmUsername = "confirm_username"
	UserPromptTypeEnterName       = "enter_name"
	UserPromptTypeEnterCompany    = "enter_company"

	// Trigger Disable Reasons
	TriggerDisableReasonBuildFailures    = "successive_build_failures"
	TriggerDisableReasonInternalErrors   = "successive_build_internal_errors"
	TriggerDisableReasonUserToggled      = "user_toggled"

	// Service Key Approval Types
	ServiceKeyApprovalTypeSuperUser  = "Super User API"
	ServiceKeyApprovalTypeKeyRotation = "Key Rotation"
	ServiceKeyApprovalTypeAutomatic   = "Automatic"
)

// Helper functions for enum lookups
func IsTerminalBuildPhase(phase string) bool {
	return phase == BuildPhaseComplete ||
		phase == BuildPhaseError ||
		phase == BuildPhaseInternalError ||
		phase == BuildPhaseCancelled
}

// Common request/response structures
type CreateUserRequest struct {
	Username string `json:"username" validate:"required"`
	Email    string `json:"email" validate:"required,email"`
	Password string `json:"password" validate:"required,min=8"`
}

type CreateRepositoryRequest struct {
	Name        string `json:"name" validate:"required"`
	Namespace   string `json:"namespace" validate:"required"`
	Visibility  string `json:"visibility" validate:"required,oneof=public private"`
	Description string `json:"description"`
	RepoKind    string `json:"repo_kind" validate:"required,oneof=image application"`
}

type CreateTeamRequest struct {
	Name        string `json:"name" validate:"required"`
	Role        string `json:"role" validate:"required"`
	Description string `json:"description"`
}

type LoginRequest struct {
	Username string `json:"username" validate:"required"`
	Password string `json:"password" validate:"required"`
}

type LoginResponse struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
	User      User      `json:"user"`
}

type ErrorResponse struct {
	Error       string `json:"error"`
	ErrorCode   string `json:"error_code"`
	ErrorDetail string `json:"error_detail,omitempty"`
}

type PaginatedResponse struct {
	Data          interface{} `json:"data"`
	HasAdditional bool        `json:"has_additional"`
	Page          int         `json:"page"`
	PageSize      int         `json:"page_size"`
}

// Extended response types with computed fields
type UserResponse struct {
	User
	IsRobot       bool   `json:"is_robot"`
	IsOrg         bool   `json:"is_org"`
	CanCreateRepo bool   `json:"can_create_repo"`
	Avatar        string `json:"avatar"`
}

type RepositoryResponse struct {
	Repository
	Namespace      string    `json:"namespace"`
	IsPublic       bool      `json:"is_public"`
	IsStarred      bool      `json:"is_starred"`
	TagCount       int       `json:"tag_count"`
	LastModified   time.Time `json:"last_modified"`
	Size           int64     `json:"size"`
	PullCount      int       `json:"pull_count"`
	CanRead        bool      `json:"can_read"`
	CanWrite       bool      `json:"can_write"`
	CanAdmin       bool      `json:"can_admin"`
	StatusToken    string    `json:"status_token"`
	TrustEnabled   bool      `json:"trust_enabled"`
	State          string    `json:"state"`
	Kind           string    `json:"kind"`
}

type TagResponse struct {
	Tag
	ManifestDigest string            `json:"manifest_digest"`
	ImageID        string            `json:"image_id"`
	Size           int64             `json:"size"`
	LastModified   time.Time         `json:"last_modified"`
	DockerImageID  string            `json:"docker_image_id"`
	IsManifestList bool              `json:"is_manifest_list"`
	Labels         map[string]string `json:"labels"`
	Expiration     *time.Time        `json:"expiration"`
}

type ManifestResponse struct {
	Manifest
	Tags           []string          `json:"tags"`
	Size           int64             `json:"size"`
	LastModified   time.Time         `json:"last_modified"`
	Labels         map[string]string `json:"labels"`
	IsManifestList bool              `json:"is_manifest_list"`
	Vulnerabilities interface{}       `json:"vulnerabilities,omitempty"`
}

type BuildResponse struct {
	RepositoryBuild
	CanRead     bool   `json:"can_read"`
	CanCancel   bool   `json:"can_cancel"`
	Repository  string `json:"repository"`
	Namespace   string `json:"namespace"`
	StatusToken string `json:"status_token"`
	Archive     bool   `json:"archive"`
}