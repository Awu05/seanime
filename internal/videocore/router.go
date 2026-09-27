package videocore

import (
	"encoding/json"
	"seanime/internal/events"
	"sync"

	"github.com/rs/zerolog"
)

// ClientRouter is the single subscriber to browser player events, delivering each one to the
// VideoCore responsible for that client: the core that sent the client its current stream, else
// the client's profile session core, else the app-wide core. Cores can't subscribe themselves -
// the websocket manager keys subscribers by name, so only the last one would hear anything.
type ClientRouter struct {
	ws     events.WSEventManagerInterface
	logger *zerolog.Logger

	mu        sync.RWMutex
	fallback  *VideoCore
	byProfile map[string]*VideoCore
	bound     map[string]*VideoCore // client ID -> core that sent the client its current stream
}

func NewClientRouter(ws events.WSEventManagerInterface, logger *zerolog.Logger) *ClientRouter {
	r := &ClientRouter{
		ws:        ws,
		logger:    logger,
		byProfile: make(map[string]*VideoCore),
		bound:     make(map[string]*VideoCore),
	}
	sub := ws.SubscribeToClientVideoCoreEvents("videocore")
	go func() {
		for event := range sub.Channel {
			r.route(event)
		}
	}()
	return r
}

func (r *ClientRouter) SetFallback(vc *VideoCore) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.fallback = vc
}

func (r *ClientRouter) RegisterProfile(profileID string, vc *VideoCore) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.byProfile[profileID] = vc
}

func (r *ClientRouter) route(event *events.WebsocketClientEvent) {
	playerEvent := &ClientEvent{}
	marshaled, _ := json.Marshal(event.Payload)
	_ = json.Unmarshal(marshaled, playerEvent)

	clientID := event.ClientID
	if clientID == "" {
		clientID = playerEvent.ClientId
	}

	core := r.coreFor(clientID)
	if core == nil {
		return
	}
	if !core.clientPlayerEventSubscriber.Send(event) {
		r.logger.Warn().Str("clientId", clientID).Msgf("videocore: Player event channel full, dropped %s", playerEvent.Type)
	}
	if playerEvent.Type == PlayerEventVideoTerminated {
		r.unbind(clientID, core)
	}
}

func (r *ClientRouter) coreFor(clientID string) *VideoCore {
	r.mu.RLock()
	core, ok := r.bound[clientID]
	r.mu.RUnlock()
	if ok {
		return core
	}

	// Looked up only when unclaimed: it takes the websocket manager's lock, also held during writes.
	profileID, hasProfile := r.ws.GetClientProfileID(clientID)

	r.mu.RLock()
	defer r.mu.RUnlock()
	if core, ok := r.byProfile[profileID]; hasProfile && ok {
		return core
	}
	return r.fallback
}

func (r *ClientRouter) bind(clientID string, vc *VideoCore) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.bound[clientID] = vc
}

// unbind releases clientID only if vc still owns it, so a stale release can't drop a newer claim.
func (r *ClientRouter) unbind(clientID string, vc *VideoCore) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.bound[clientID] == vc {
		delete(r.bound, clientID)
	}
}

func (r *ClientRouter) unregister(vc *VideoCore) {
	r.mu.Lock()
	for profileID, core := range r.byProfile {
		if core == vc {
			delete(r.byProfile, profileID)
		}
	}
	for clientID, core := range r.bound {
		if core == vc {
			delete(r.bound, clientID)
		}
	}
	r.mu.Unlock()
	vc.clientPlayerEventSubscriber.Close()
}

// ClaimClient routes clientId's browser player events to this core until that player terminates
// or another core sends the client a stream.
func (vc *VideoCore) ClaimClient(clientId string) {
	vc.router.bind(clientId, vc)
}

// DetachFromRouter stops routing player events to this core and ends its client event listener.
func (vc *VideoCore) DetachFromRouter() {
	vc.router.unregister(vc)
}
