package service

import "errors"

var (
	ErrNoAvailableAccount = errors.New("没有可用token")
	ErrNoPermission       = errors.New("没有账号有权限使用此模型")
	ErrTokenExpired       = errors.New("token已过期")
	ErrRequestFailed      = errors.New("请求失败")
)
