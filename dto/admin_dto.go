package dto

import "chronosphere/domain"

type UpdateAdminProfileRequest struct {
	Name   string `json:"name" binding:"required,min=3,max=50"`
	Phone  string `json:"phone" binding:"required,min=8,max=20"`
	Image  string `json:"image" binding:"omitempty,url"`
	Gender string `json:"gender" binding:"required,oneof=male female"`
}

func MakeUpdateAdminProfileRequest(req *UpdateAdminProfileRequest) domain.User {
	return domain.User{
		Name:   req.Name,
		Phone:  req.Phone,
		Image:  &req.Image,
		Gender: req.Gender,
	}
}
