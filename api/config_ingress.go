package api

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/dolfly/x/config"
	parser "github.com/dolfly/x/config/parsing/ingress"
	"github.com/dolfly/x/registry"
	"github.com/gin-gonic/gin"
)

// swagger:parameters getIngressListRequest
type getIngressListRequest struct {
}

// successful operation.
// swagger:response getIngressListResponse
type getIngressListResponse struct {
	// in: body
	Data ingressList
}

type ingressList struct {
	Count int                     `json:"count"`
	List  []*config.IngressConfig `json:"list"`
}

func getIngressList(ctx *gin.Context) {
	// swagger:route GET /config/ingresses Ingress getIngressListRequest
	//
	// Get ingress list.
	//
	//     Security:
	//       basicAuth: []
	//
	//     Responses:
	//       200: getIngressListResponse

	var req getIngressListRequest
	ctx.ShouldBindQuery(&req)

	list := config.Global().Ingresses

	var resp getIngressListResponse
	resp.Data = ingressList{
		Count: len(list),
		List:  list,
	}

	ctx.JSON(http.StatusOK, Response{
		Data: resp.Data,
	})
}

// swagger:parameters getIngressRequest
type getIngressRequest struct {
	// in: path
	// required: true
	Ingress string `uri:"ingress" json:"ingress"`
}

// successful operation.
// swagger:response getIngressResponse
type getIngressResponse struct {
	// in: body
	Data *config.IngressConfig
}

func getIngress(ctx *gin.Context) {
	// swagger:route GET /config/ingresses/{ingress} Ingress getIngressRequest
	//
	// Get ingress.
	//
	//     Security:
	//       basicAuth: []
	//
	//     Responses:
	//       200: getIngressResponse

	var req getIngressRequest
	ctx.ShouldBindUri(&req)

	var resp getIngressResponse

	for _, ingress := range config.Global().Ingresses {
		if ingress == nil {
			continue
		}
		if ingress.Name == req.Ingress {
			resp.Data = ingress
		}
	}

	ctx.JSON(http.StatusOK, Response{
		Data: resp.Data,
	})
}

// swagger:parameters createIngressRequest
type createIngressRequest struct {
	// in: body
	Data config.IngressConfig `json:"data"`
}

// successful operation.
// swagger:response createIngressResponse
type createIngressResponse struct {
	Data Response
}

func createIngress(ctx *gin.Context) {
	// swagger:route POST /config/ingresses Ingress createIngressRequest
	//
	// Create a new ingress, the name of the ingress must be unique in ingress list.
	//
	//     Security:
	//       basicAuth: []
	//
	//     Responses:
	//       200: createIngressResponse

	var req createIngressRequest
	ctx.ShouldBindJSON(&req.Data)

	name := strings.TrimSpace(req.Data.Name)
	if name == "" {
		writeError(ctx, NewError(http.StatusBadRequest, ErrCodeInvalid, "ingress name is required"))
		return
	}
	req.Data.Name = name

	if registry.IngressRegistry().IsRegistered(name) {
		writeError(ctx, NewError(http.StatusBadRequest, ErrCodeDup, fmt.Sprintf("ingress %s already exists", name)))
		return
	}

	v := parser.ParseIngress(&req.Data)

	if err := registry.IngressRegistry().Register(name, v); err != nil {
		writeError(ctx, NewError(http.StatusBadRequest, ErrCodeDup, fmt.Sprintf("ingress %s already exists", name)))
		return
	}

	config.OnUpdate(func(c *config.Config) error {
		c.Ingresses = append(c.Ingresses, &req.Data)
		return nil
	})

	ctx.JSON(http.StatusOK, Response{
		Msg: "OK",
	})
}

// swagger:parameters updateIngressRequest
type updateIngressRequest struct {
	// in: path
	// required: true
	Ingress string `uri:"ingress" json:"ingress"`
	// in: body
	Data config.IngressConfig `json:"data"`
}

// successful operation.
// swagger:response updateIngressResponse
type updateIngressResponse struct {
	Data Response
}

func updateIngress(ctx *gin.Context) {
	// swagger:route PUT /config/ingresses/{ingress} Ingress updateIngressRequest
	//
	// Update ingress by name, the ingress must already exist.
	//
	//     Security:
	//       basicAuth: []
	//
	//     Responses:
	//       200: updateIngressResponse

	var req updateIngressRequest
	ctx.ShouldBindUri(&req)
	ctx.ShouldBindJSON(&req.Data)

	name := strings.TrimSpace(req.Ingress)

	if !registry.IngressRegistry().IsRegistered(name) {
		writeError(ctx, NewError(http.StatusBadRequest, ErrCodeNotFound, fmt.Sprintf("ingress %s not found", name)))
		return
	}

	req.Data.Name = name

	v := parser.ParseIngress(&req.Data)

	registry.IngressRegistry().Unregister(name)

	if err := registry.IngressRegistry().Register(name, v); err != nil {
		writeError(ctx, NewError(http.StatusBadRequest, ErrCodeDup, fmt.Sprintf("ingress %s already exists", name)))
		return
	}

	config.OnUpdate(func(c *config.Config) error {
		for i := range c.Ingresses {
			if c.Ingresses[i].Name == name {
				c.Ingresses[i] = &req.Data
				break
			}
		}
		return nil
	})

	ctx.JSON(http.StatusOK, Response{
		Msg: "OK",
	})
}

// swagger:parameters deleteIngressRequest
type deleteIngressRequest struct {
	// in: path
	// required: true
	Ingress string `uri:"ingress" json:"ingress"`
}

// successful operation.
// swagger:response deleteIngressResponse
type deleteIngressResponse struct {
	Data Response
}

func deleteIngress(ctx *gin.Context) {
	// swagger:route DELETE /config/ingresses/{ingress} Ingress deleteIngressRequest
	//
	// Delete ingress by name.
	//
	//     Security:
	//       basicAuth: []
	//
	//     Responses:
	//       200: deleteIngressResponse

	var req deleteIngressRequest
	ctx.ShouldBindUri(&req)

	name := strings.TrimSpace(req.Ingress)

	if !registry.IngressRegistry().IsRegistered(name) {
		writeError(ctx, NewError(http.StatusBadRequest, ErrCodeNotFound, fmt.Sprintf("ingress %s not found", name)))
		return
	}
	registry.IngressRegistry().Unregister(name)

	config.OnUpdate(func(c *config.Config) error {
		ingresses := c.Ingresses
		c.Ingresses = nil
		for _, s := range ingresses {
			if s.Name == name {
				continue
			}
			c.Ingresses = append(c.Ingresses, s)
		}
		return nil
	})

	ctx.JSON(http.StatusOK, Response{
		Msg: "OK",
	})
}
