package siteproxy

import (
	"context"
	"encoding/base64"
	"errors"

	"magpiesync/internal/netproxy"
	"magpiesync/internal/store"
	"magpiesync/internal/vault"
)

func Key(site string) string { return "network.proxy." + site }

func aad(site string) string { return "proxy:site:" + site }

// Encode validates and encrypts a proxy with its owning site's identity.
func Encode(v *vault.Vault, site, raw string) (string, error) {
	canonical, err := netproxy.Normalize(raw)
	if err != nil {
		return "", errors.New("代理地址无效，请使用 HTTP(S) 或 SOCKS5 代理地址")
	}
	if canonical == "" {
		return "", nil
	}
	if v == nil {
		return "", errors.New("无法加密站点代理")
	}
	ciphertext, err := v.Encrypt([]byte(canonical), aad(site))
	if err != nil {
		return "", errors.New("无法加密站点代理")
	}
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// Load reads the current saved proxy for every new login, sync or download client.
func Load(ctx context.Context, st *store.Store, v *vault.Vault, site string) (string, error) {
	encoded, ok, err := st.GetSetting(ctx, Key(site))
	if err != nil {
		return "", errors.New("无法读取站点代理")
	}
	if !ok || encoded == "" {
		return "", nil
	}
	ciphertext, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || v == nil {
		return "", errors.New("无法解密站点代理")
	}
	plain, err := v.Decrypt(ciphertext, aad(site))
	if err != nil {
		return "", errors.New("无法解密站点代理")
	}
	return string(plain), nil
}
