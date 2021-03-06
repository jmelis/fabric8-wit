package controller

import (
	"fmt"
	"net/http"

	"github.com/fabric8-services/fabric8-wit/app"
	"github.com/fabric8-services/fabric8-wit/application"
	"github.com/fabric8-services/fabric8-wit/errors"
	"github.com/fabric8-services/fabric8-wit/jsonapi"
	"github.com/fabric8-services/fabric8-wit/login"
	"github.com/fabric8-services/fabric8-wit/rest"
	"github.com/fabric8-services/fabric8-wit/workitem/link"
	errs "github.com/pkg/errors"

	"github.com/goadesign/goa"
	uuid "github.com/satori/go.uuid"
)

// WorkItemLinkTypeController implements the work-item-link-type resource.
type WorkItemLinkTypeController struct {
	*goa.Controller
	db     application.DB
	config WorkItemLinkTypeControllerConfiguration
}

// WorkItemLinkTypeControllerConfiguration the configuration for the WorkItemLinkTypeController
type WorkItemLinkTypeControllerConfiguration interface {
	GetCacheControlWorkItemLinkTypes() string
	GetCacheControlWorkItemLinkType() string
}

// NewWorkItemLinkTypeController creates a work-item-link-type controller.
func NewWorkItemLinkTypeController(service *goa.Service, db application.DB, config WorkItemLinkTypeControllerConfiguration) *WorkItemLinkTypeController {
	return &WorkItemLinkTypeController{
		Controller: service.NewController("WorkItemLinkTypeController"),
		db:         db,
		config:     config,
	}
}

// enrichLinkTypeSingle includes related resources in the single's "included" array
func enrichLinkTypeSingle(ctx *workItemLinkContext, single *app.WorkItemLinkTypeSingle) error {
	// Add "links" element
	relatedURL := rest.AbsoluteURL(ctx.Request, ctx.LinkFunc(*single.Data.ID))
	single.Data.Links = &app.GenericLinks{
		Self:    &relatedURL,
		Related: &relatedURL,
	}

	// Now include the optional link category data in the work item link type "included" array
	modelCategory, err := ctx.Application.WorkItemLinkCategories().Load(ctx.Context, single.Data.Relationships.LinkCategory.Data.ID)
	if err != nil {
		return err
	}
	appCategory := ConvertLinkCategoryFromModel(*modelCategory)
	single.Included = append(single.Included, appCategory.Data)

	// Now include the optional link space data in the work item link type "included" array
	space, err := ctx.Application.Spaces().Load(ctx.Context, *single.Data.Relationships.Space.Data.ID)
	if err != nil {
		return err
	}

	spaceData, err := ConvertSpaceFromModel(ctx.Request, *space, IncludeBacklogTotalCount(ctx.Context, ctx.DB))
	if err != nil {
		return err
	}
	spaceSingle := &app.SpaceSingle{
		Data: spaceData,
	}
	single.Included = append(single.Included, spaceSingle.Data)

	return nil
}

// enrichLinkTypeList includes related resources in the list's "included" array
func enrichLinkTypeList(ctx *workItemLinkContext, list *app.WorkItemLinkTypeList) error {
	// Add "links" element
	for _, data := range list.Data {
		relatedURL := rest.AbsoluteURL(ctx.Request, ctx.LinkFunc(*data.ID))
		data.Links = &app.GenericLinks{
			Self:    &relatedURL,
			Related: &relatedURL,
		}
	}
	// Build our "set" of distinct category IDs already converted as strings
	categoryIDMap := map[uuid.UUID]bool{}
	for _, typeData := range list.Data {
		categoryIDMap[typeData.Relationships.LinkCategory.Data.ID] = true
	}
	// Now include the optional link category data in the work item link type "included" array
	for categoryID := range categoryIDMap {
		modelCategory, err := ctx.Application.WorkItemLinkCategories().Load(ctx.Context, categoryID)
		if err != nil {
			return err
		}
		appCategory := ConvertLinkCategoryFromModel(*modelCategory)
		list.Included = append(list.Included, appCategory.Data)
	}

	// Build our "set" of distinct space IDs already converted as strings
	spaceIDMap := map[uuid.UUID]bool{}
	for _, typeData := range list.Data {
		spaceIDMap[*typeData.Relationships.Space.Data.ID] = true
	}
	// Now include the optional link space data in the work item link type "included" array
	for spaceID := range spaceIDMap {
		space, err := ctx.Application.Spaces().Load(ctx.Context, spaceID)
		if err != nil {
			return err
		}
		spaceData, err := ConvertSpaceFromModel(ctx.Request, *space, IncludeBacklogTotalCount(ctx.Context, ctx.DB))
		if err != nil {
			return err
		}
		spaceSingle := &app.SpaceSingle{
			Data: spaceData,
		}
		list.Included = append(list.Included, spaceSingle.Data)
	}
	return nil
}

// Create runs the create action.
func (c *WorkItemLinkTypeController) Create(ctx *app.CreateWorkItemLinkTypeContext) error {
	// Currently not used. Disabled as part of https://github.com/fabric8-services/fabric8-wit/issues/1299
	if true {
		return ctx.MethodNotAllowed()
	}
	// Convert payload from app to model representation
	appLinkType := app.WorkItemLinkTypeSingle{
		Data: ctx.Payload.Data,
	}
	// Set the space to the Payload
	if ctx.Payload.Data != nil && ctx.Payload.Data.Relationships != nil {
		// We overwrite or use the space ID in the URL to set the space of this WI
		spaceSelfURL := rest.AbsoluteURL(ctx.Request, app.SpaceHref(ctx.SpaceID.String()))
		ctx.Payload.Data.Relationships.Space = app.NewSpaceRelation(ctx.SpaceID, spaceSelfURL)
	}
	modelLinkType, err := ConvertWorkItemLinkTypeToModel(appLinkType)
	if err != nil {
		return jsonapi.JSONErrorResponse(ctx, goa.ErrBadRequest(err.Error()))
	}
	modelLinkType.SpaceID = ctx.SpaceID
	currentUserIdentityID, err := login.ContextIdentity(ctx)
	if err != nil {
		return jsonapi.JSONErrorResponse(ctx, errors.NewUnauthorizedError(err.Error()))
	}
	var createdModelLinkType *link.WorkItemLinkType
	err = application.Transactional(c.db, func(appl application.Application) error {
		createdModelLinkType, err = appl.WorkItemLinkTypes().Create(ctx.Context, modelLinkType)
		if err != nil {
			return err
		}
		appLinkType = ConvertWorkItemLinkTypeFromModel(ctx.Request, *createdModelLinkType)
		// Enrich
		HrefFunc := func(obj interface{}) string {
			return fmt.Sprintf(app.WorkItemLinkTypeHref(createdModelLinkType.SpaceID, "%v"), obj)
		}
		linkCtx := newWorkItemLinkContext(ctx.Context, ctx.Service, appl, c.db, ctx.Request, ctx.ResponseWriter, HrefFunc, currentUserIdentityID)
		return enrichLinkTypeSingle(linkCtx, &appLinkType)
	})
	if err != nil {
		return jsonapi.JSONErrorResponse(ctx, err)
	}
	ctx.ResponseData.Header().Set("Location", app.WorkItemLinkTypeHref(createdModelLinkType.SpaceID, appLinkType.Data.ID))
	return ctx.Created(&appLinkType)
}

// Delete runs the delete action.
func (c *WorkItemLinkTypeController) Delete(ctx *app.DeleteWorkItemLinkTypeContext) error {
	// Currently not used. Disabled as part of https://github.com/fabric8-services/fabric8-wit/issues/1299
	if true {
		return ctx.MethodNotAllowed()
	}
	err := application.Transactional(c.db, func(appl application.Application) error {
		err := appl.WorkItemLinkTypes().Delete(ctx.Context, ctx.SpaceID, ctx.WiltID)
		if err != nil {
			return err
		}
		return ctx.OK([]byte{})
	})
	if err != nil {
		return jsonapi.JSONErrorResponse(ctx, err)
	}
	return nil
}

// List runs the list action.
func (c *WorkItemLinkTypeController) List(ctx *app.ListWorkItemLinkTypeContext) error {
	var modelLinkTypes []link.WorkItemLinkType
	err := application.Transactional(c.db, func(appl application.Application) error {
		var err error
		modelLinkTypes, err = appl.WorkItemLinkTypes().List(ctx.Context, ctx.SpaceID)
		return err
	})
	if err != nil {
		return jsonapi.JSONErrorResponse(ctx, err)
	}
	return ctx.ConditionalEntities(modelLinkTypes, c.config.GetCacheControlWorkItemLinkTypes, func() error {
		// convert to rest representation
		appLinkTypes := app.WorkItemLinkTypeList{}
		appLinkTypes.Data = make([]*app.WorkItemLinkTypeData, len(modelLinkTypes))
		for index, modelLinkType := range modelLinkTypes {
			appLinkType := ConvertWorkItemLinkTypeFromModel(ctx.Request, modelLinkType)
			appLinkTypes.Data[index] = appLinkType.Data
		}
		// TODO: When adding pagination, this must not be len(rows) but
		// the overall total number of elements from all pages.
		appLinkTypes.Meta = &app.WorkItemLinkTypeListMeta{
			TotalCount: len(modelLinkTypes),
		}
		// Enrich
		HrefFunc := func(obj interface{}) string {
			return fmt.Sprintf(app.WorkItemLinkTypeHref(ctx.SpaceID, "%v"), obj)
		}
		err := application.Transactional(c.db, func(appl application.Application) error {
			linkCtx := newWorkItemLinkContext(ctx.Context, ctx.Service, appl, c.db, ctx.Request, ctx.ResponseWriter, HrefFunc, nil)
			return enrichLinkTypeList(linkCtx, &appLinkTypes)
		})
		if err != nil {
			return errs.Wrap(err, "Failed to enrich link types")
		}
		return ctx.OK(&appLinkTypes)
	})
}

// Show runs the show action.
func (c *WorkItemLinkTypeController) Show(ctx *app.ShowWorkItemLinkTypeContext) error {
	err := application.Transactional(c.db, func(appl application.Application) error {
		modelLinkType, err := appl.WorkItemLinkTypes().Load(ctx.Context, ctx.WiltID)
		if err != nil {
			return jsonapi.JSONErrorResponse(ctx, err)
		}
		return ctx.ConditionalRequest(*modelLinkType, c.config.GetCacheControlWorkItemLinkType, func() error {
			// Convert the created link type entry into a rest representation
			appLinkType := ConvertWorkItemLinkTypeFromModel(ctx.Request, *modelLinkType)

			// Enrich
			HrefFunc := func(obj interface{}) string {
				return fmt.Sprintf(app.WorkItemLinkTypeHref(ctx.SpaceID, "%v"), obj)
			}
			linkCtx := newWorkItemLinkContext(ctx.Context, ctx.Service, appl, c.db, ctx.Request, ctx.ResponseWriter, HrefFunc, nil)
			err = enrichLinkTypeSingle(linkCtx, &appLinkType)
			if err != nil {
				return goa.ErrInternal("Failed to enrich link type: %s", err.Error())
			}
			return ctx.OK(&appLinkType)
		})
	})
	if err != nil {
		return jsonapi.JSONErrorResponse(ctx, err)
	}
	return nil
}

// Update runs the update action.
func (c *WorkItemLinkTypeController) Update(ctx *app.UpdateWorkItemLinkTypeContext) error {
	// Currently not used. Disabled as part of https://github.com/fabric8-services/fabric8-wit/issues/1299
	if true {
		return ctx.MethodNotAllowed()
	}
	currentUserIdentityID, err := login.ContextIdentity(ctx)
	if err != nil {
		return jsonapi.JSONErrorResponse(ctx, errors.NewUnauthorizedError(err.Error()))
	}
	var appLinkType app.WorkItemLinkTypeSingle
	err = application.Transactional(c.db, func(appl application.Application) error {
		toSave := app.WorkItemLinkTypeSingle{
			Data: ctx.Payload.Data,
		}
		if toSave.Data.ID == nil {
			return errors.NewBadParameterError("work item link type", nil)
		}
		modelLinkTypeToSave, err := ConvertWorkItemLinkTypeToModel(toSave)
		if err != nil {
			return err
		}
		modelLinkTypeSaved, err := appl.WorkItemLinkTypes().Save(ctx.Context, *modelLinkTypeToSave)
		if err != nil {
			return err
		}
		appLinkType = ConvertWorkItemLinkTypeFromModel(ctx.Request, *modelLinkTypeSaved)
		// Enrich
		HrefFunc := func(obj interface{}) string {
			return fmt.Sprintf(app.WorkItemLinkTypeHref(ctx.SpaceID, "%v"), obj)
		}
		linkTypeCtx := newWorkItemLinkContext(ctx.Context, ctx.Service, appl, c.db, ctx.Request, ctx.ResponseWriter, HrefFunc, currentUserIdentityID)
		return enrichLinkTypeSingle(linkTypeCtx, &appLinkType)
	})
	if err != nil {
		return jsonapi.JSONErrorResponse(ctx, err)
	}
	return ctx.OK(&appLinkType)
}

// ConvertWorkItemLinkTypeFromModel converts a work item link type from model to REST representation
func ConvertWorkItemLinkTypeFromModel(request *http.Request, modelLinkType link.WorkItemLinkType) app.WorkItemLinkTypeSingle {
	spaceRelatedURL := rest.AbsoluteURL(request, app.SpaceHref(modelLinkType.SpaceID.String()))
	linkCategoryRelatedURL := rest.AbsoluteURL(request, app.WorkItemLinkCategoryHref(modelLinkType.LinkCategoryID.String()))

	topologyStr := modelLinkType.Topology.String()
	var converted = app.WorkItemLinkTypeSingle{
		Data: &app.WorkItemLinkTypeData{
			Type: link.EndpointWorkItemLinkTypes,
			ID:   &modelLinkType.ID,
			Attributes: &app.WorkItemLinkTypeAttributes{
				Name:        &modelLinkType.Name,
				Description: modelLinkType.Description,
				Version:     &modelLinkType.Version,
				CreatedAt:   &modelLinkType.CreatedAt,
				UpdatedAt:   &modelLinkType.UpdatedAt,
				ForwardName: &modelLinkType.ForwardName,
				ReverseName: &modelLinkType.ReverseName,
				Topology:    &topologyStr,
			},
			Relationships: &app.WorkItemLinkTypeRelationships{
				LinkCategory: &app.RelationWorkItemLinkCategory{
					Data: &app.RelationWorkItemLinkCategoryData{
						Type: link.EndpointWorkItemLinkCategories,
						ID:   modelLinkType.LinkCategoryID,
					},
					Links: &app.GenericLinks{
						Self:    &linkCategoryRelatedURL,
						Related: &linkCategoryRelatedURL,
					},
				},
				Space: app.NewSpaceRelation(modelLinkType.SpaceID, spaceRelatedURL),
			},
		},
	}
	return converted
}

// ConvertWorkItemLinkTypeToModel converts the incoming app representation of a work item link type to the model layout.
// Values are only overwrriten if they are set in "in", otherwise the values in "out" remain.
func ConvertWorkItemLinkTypeToModel(appLinkType app.WorkItemLinkTypeSingle) (*link.WorkItemLinkType, error) {
	modelLinkType := link.WorkItemLinkType{}
	if appLinkType.Data == nil {
		return nil, errors.NewBadParameterError("data", nil).Expected("not <nil>")
	}
	if appLinkType.Data.Attributes == nil {
		return nil, errors.NewBadParameterError("data.attributes", nil).Expected("not <nil>")
	}
	if appLinkType.Data.Relationships == nil {
		return nil, errors.NewBadParameterError("data.relationships", nil).Expected("not <nil>")
	}

	attrs := appLinkType.Data.Attributes
	rel := appLinkType.Data.Relationships

	if appLinkType.Data.ID != nil {
		modelLinkType.ID = *appLinkType.Data.ID
	}

	if attrs != nil {
		// If the name is not nil, it MUST NOT be empty
		if attrs.Name != nil {
			if *attrs.Name == "" {
				return nil, errors.NewBadParameterError("data.attributes.name", *attrs.Name)
			}
			modelLinkType.Name = *attrs.Name
		}

		if attrs.Description != nil {
			modelLinkType.Description = attrs.Description
		}

		if attrs.Version != nil {
			modelLinkType.Version = *attrs.Version
		}

		// If the forwardName is not nil, it MUST NOT be empty
		if attrs.ForwardName != nil {
			if *attrs.ForwardName == "" {
				return nil, errors.NewBadParameterError("data.attributes.forward_name", *attrs.ForwardName)
			}
			modelLinkType.ForwardName = *attrs.ForwardName
		}

		// If the ReverseName is not nil, it MUST NOT be empty
		if attrs.ReverseName != nil {
			if *attrs.ReverseName == "" {
				return nil, errors.NewBadParameterError("data.attributes.reverse_name", *attrs.ReverseName)
			}
			modelLinkType.ReverseName = *attrs.ReverseName
		}

		if attrs.Topology != nil {
			modelLinkType.Topology = link.Topology(*attrs.Topology)
			if err := modelLinkType.Topology.CheckValid(); err != nil {
				return nil, err
			}
		}
	}

	if rel != nil && rel.LinkCategory != nil && rel.LinkCategory.Data != nil {
		modelLinkType.LinkCategoryID = rel.LinkCategory.Data.ID
	}
	if rel != nil && rel.Space != nil && rel.Space.Data != nil {
		modelLinkType.SpaceID = *rel.Space.Data.ID
	}

	return &modelLinkType, nil
}

func ConvertLinkTypesFromModels(request *http.Request, modelLinkTypes []link.WorkItemLinkType) (*app.WorkItemLinkTypeList, error) {
	appLinkTypes := app.WorkItemLinkTypeList{}
	appLinkTypes.Data = make([]*app.WorkItemLinkTypeData, len(modelLinkTypes))
	for index, modelLinkType := range modelLinkTypes {
		appLinkType := ConvertWorkItemLinkTypeFromModel(request, modelLinkType)
		appLinkTypes.Data[index] = appLinkType.Data
	}
	// TODO: When adding pagination, this must not be len(rows) but
	// the overall total number of elements from all pages.
	appLinkTypes.Meta = &app.WorkItemLinkTypeListMeta{
		TotalCount: len(modelLinkTypes),
	}
	return &appLinkTypes, nil
}
