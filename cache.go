package middleware

import (
	"bytes"
	"crypto/sha256"
	"gitea.com/go-chi/cache"
	"github.com/go-chi/chi/v5/middleware"
	"net/http"
	"time"
)

const cacheStatusHeaderKey = "x-cache-status"

var globalCache *Cache

type cachedResponse struct {
	Status   int
	Response *bytes.Buffer
	Header   http.Header
}

type Cache struct {
	c           cache.Cache
	opts        cache.Options
	ttl         time.Duration
	negativeTtl time.Duration
}

func (c *Cache) Put(key string, val interface{}) error {
	return c.c.Put(key, val, int64(c.ttl.Seconds()))
}

func (c *Cache) Get(key string) interface{} {
	return c.c.Get(key)
}

func createCacheKey(r *http.Request) string {
	h := sha256.New()
	h.Write([]byte(r.URL.String()))
	return string(h.Sum(nil))
}

func NewCache(opts cache.Options, TTL time.Duration, negativeTTL time.Duration) (cm *Cache, err error) {
	cm = &Cache{ttl: TTL, negativeTtl: negativeTTL}
	cm.c, err = cache.NewCacher(opts)
	globalCache = cm
	return cm, err
}

func WithCache(c *Cache) func(http.Handler) http.Handler {
	if c == nil {
		c = globalCache
	}
	return withCache(c)
}

func WithGlobalCache() func(http.Handler) http.Handler {
	return withCache(globalCache)
}

func withCache(c *Cache) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
			cacheKey := createCacheKey(r)

			if !c.c.IsExist(cacheKey) {
				responseToCache := new(bytes.Buffer)
				ww := middleware.NewWrapResponseWriter(rw, r.ProtoMajor)
				ww.Tee(responseToCache)
				ww.Header().Add(cacheStatusHeaderKey, "MISS")

				defer func() {
					cr := cachedResponse{Status: ww.Status(), Response: responseToCache, Header: ww.Header()}
					ttl := c.ttl.Seconds()
					if ww.Status() >= 400 {
						ttl = c.negativeTtl.Seconds()
					}

					if err := c.c.Put(cacheKey, cr, int64(ttl)); err != nil {
						// TODO: handle err
					}
				}()

				next.ServeHTTP(ww, r)
			} else {
				cr := c.c.Get(cacheKey).(cachedResponse)

				for headerName := range cr.Header {
					rw.Header().Set(headerName, cr.Header.Get(headerName))
					// TODO handle headers with []values
				}
				rw.Header().Set(cacheStatusHeaderKey, "HIT")

				rw.WriteHeader(cr.Status)
				_, _ = rw.Write(cr.Response.Bytes()) // TODO: handle err / maybe check bytes written with cachedResponse length
			}
		})
	}
}
