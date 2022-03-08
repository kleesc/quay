"""
Manage organizations, members and OAuth applications.
"""

import logging

from flask import request

import features
from auth.permissions import (
    AdministerOrganizationPermission,
    SuperUserPermission,
    OrganizationMemberPermission,
    UserReadPermission,
)
from data import model
from data.model import config
from endpoints.api import (
    resource,
    nickname,
    ApiResource,
    validate_json_request,
    request_error,
    require_user_admin,
    require_scope,
    show_if,
)
from endpoints.exception import InvalidToken, Unauthorized
from auth import scopes


logger = logging.getLogger(__name__)


@resource("/v1/organization/<orgname>/quota")
@show_if(features.QUOTA_MANAGEMENT)
class OrganizationQuota(ApiResource):
    schemas = {
        "NewOrgQuota": {
            "type": "object",
            "description": "Description of a new organization quota",
            "required": ["limit_bytes"],
            "properties": {
                "limit_bytes": {
                    "type": "integer",
                    "description": "Number of bytes the organization is allowed",
                },
            },
        },
    }

    @nickname("getOrganizationQuota")
    def get(self, orgname):
        orgperm = OrganizationMemberPermission(orgname)

        if not orgperm.can():
            raise Unauthorized()

        quota = model.namespacequota.get_namespace_quota(orgname)
        quota_limit_types = model.namespacequota.get_namespace_limit_types()

        return {
            "limit_bytes": quota.limit_bytes if quota else None,
            "quota_limit_types": quota_limit_types,
        }

    @nickname("createOrganizationQuota")
    @validate_json_request("NewOrgQuota")
    def post(self, orgname):
        """
        Create a new organization quota.
        """
        orgperm = AdministerOrganizationPermission(orgname)
        superperm = SuperUserPermission()

        if not superperm.can():
            if not orgperm.can() or not config.app_config.get("DEFAULT_SYSTEM_REJECT_QUOTA_BYTES") != 0:
                    raise Unauthorized()

        quota_data = request.get_json()

        quota = model.namespacequota.get_namespace_quota(orgname)

        if quota is not None:
            msg = "quota already exists"
            raise request_error(message=msg)

        try:
            newquota = model.namespacequota.create_namespace_quota(
                name=orgname, limit_bytes=quota_data["limit_bytes"]
            )
            if newquota is not None:
                return "Created", 201
            else:
                raise request_error("Quota Failed to Create")
        except model.DataModelException as ex:
            raise request_error(exception=ex)

    @nickname("changeOrganizationQuota")
    @validate_json_request("NewOrgQuota")
    def put(self, orgname):
        orgperm = AdministerOrganizationPermission(orgname)
        superperm = SuperUserPermission()

        if not superperm.can() or not orgperm.can():
            raise Unauthorized()

        quota_data = request.get_json()

        quota = model.namespacequota.get_namespace_quota(orgname)

        if quota is None:
            msg = "quota does not exist"
            raise request_error(message=msg)

        try:
            model.namespacequota.change_namespace_quota(orgname, quota_data["limit_bytes"])
            return "Updated", 201
        except model.DataModelException as ex:
            raise request_error(exception=ex)

    @nickname("deleteOrganizationQuota")
    def delete(self, orgname):
        orgperm = AdministerOrganizationPermission(orgname)
        superperm = SuperUserPermission()

        if not superperm.can() or not orgperm.can():
            raise Unauthorized()

        quota = model.namespacequota.get_namespace_quota(orgname)

        if quota is None:
            msg = "quota does not exist"
            raise request_error(message=msg)

        try:
            success = model.namespacequota.delete_namespace_quota(orgname)
            if success == 1:
                return "Deleted", 201

            msg = "quota failed to delete"
            raise request_error(message=msg)
        except model.DataModelException as ex:
            raise request_error(exception=ex)


@resource("/v1/organization/<orgname>/quota/limits")
@show_if(features.QUOTA_MANAGEMENT)
class OrganizationQuotaLimits(ApiResource):
    schemas = {
        "NewOrgQuotaLimit": {
            "type": "object",
            "description": "Description of a new organization quota limit threshold",
            "required": ["percent_of_limit", "quota_type_id"],
            "properties": {
                "percent_of_limit": {
                    "type": "integer",
                    "description": "Percentage of quota at which to do something",
                },
                "quota_type_id": {
                    "type": "integer",
                    "description": "Quota type Id",
                },
            },
        },
    }

    @nickname("getOrganizationQuotaLimit")
    def get(self, orgname):
        orgperm = OrganizationMemberPermission(orgname)

        if not orgperm.can():
            raise Unauthorized()

        quota_limits = list(model.namespacequota.get_namespace_limits(orgname))

        return {
            "quota_limits": [{
                "percent_of_limit": limit["percent_of_limit"],
                "limit_type": {
                    "name": limit["name"],
                    "quota_limit_id": limit["id"],
                    "quota_type_id": limit["type_id"],
                },
            } for limit in quota_limits]
        }, 200

    @nickname("createOrganizationQuotaLimit")
    @validate_json_request("NewOrgQuotaLimit")
    def post(self, orgname):
        """
        Create a new organization quota.
        """
        orgperm = AdministerOrganizationPermission(orgname)
        superperm = SuperUserPermission()

        if not superperm.can():
            if not orgperm.can() or not config.app_config.get("DEFAULT_SYSTEM_REJECT_QUOTA_BYTES") != 0:
                    raise Unauthorized()

        quota_limit_data = request.get_json()
        quota = model.namespacequota.get_namespace_limit(
            orgname, quota_limit_data["quota_type_id"], quota_limit_data["percent_of_limit"]
        )

        if quota is not None:
            msg = "quota limit already exists"
            raise request_error(message=msg)

        reject_quota = model.namespacequota.get_namespace_reject_limit(orgname)
        if reject_quota is not None and model.namespacequota.is_reject_limit_type(
            quota_limit_data["quota_type_id"]
        ):
            msg = "You can only have one Reject type of quota limit"
            raise request_error(message=msg)

        try:
            model.namespacequota.create_namespace_limit(
                orgname=orgname,
                percent_of_limit=quota_limit_data["percent_of_limit"],
                quota_type_id=quota_limit_data["quota_type_id"],
            )
            return "Created", 201
        except model.DataModelException as ex:
            raise request_error(exception=ex)

    @nickname("changeOrganizationQuotaLimit")
    @validate_json_request("NewOrgQuotaLimit")
    def put(self, orgname):
        orgperm = AdministerOrganizationPermission(orgname)
        superperm = SuperUserPermission()

        if not superperm.can() or not orgperm.can():
            raise Unauthorized()

        quota_limit_data = request.get_json()

        try:
            quota_limit_id = quota_limit_data["quota_limit_id"]
        except KeyError:
            msg = "Must supply quota_limit_id for updates"
            raise request_error(message=msg)

        quota = model.namespacequota.get_namespace_limit_from_id(orgname, quota_limit_id)

        if quota is None:
            msg = "quota limit does not exist"
            raise request_error(message=msg)

        try:
            model.namespacequota.change_namespace_quota_limit(
                orgname,
                quota_limit_data["percent_of_limit"],
                quota_limit_data["quota_type_id"],
                quota_limit_data["quota_limit_id"],
            )
            return "Updated", 201
        except model.DataModelException as ex:
            raise request_error(exception=ex)

    @nickname("deleteOrganizationQuotaLimit")
    def delete(self, orgname):
        orgperm = AdministerOrganizationPermission(orgname)
        superperm = SuperUserPermission()

        if not superperm.can() or not orgperm.can():
            raise Unauthorized()

        quota_limit_id = request.args.get("quota_limit_id", None)
        if quota_limit_id is None:
            msg = "Bad request to delete quota limit. Missing quota limit identifier."
            raise request_error(message=msg)

        quota = model.namespacequota.get_namespace_limit_from_id(orgname, quota_limit_id)

        if quota is None:
            msg = "quota does not exist"
            raise request_error(message=msg)

        try:
            success = model.namespacequota.delete_namespace_quota_limit(orgname, quota_limit_id)
            if success == 1:
                return "Deleted", 201

            msg = "quota failed to delete"
            raise request_error(message=msg)
        except model.DataModelException as ex:
            raise request_error(exception=ex)


@resource("/v1/organization/<orgname>/quota/report")
@show_if(features.QUOTA_MANAGEMENT)
class OrganizationQuotaReport(ApiResource):
    @nickname("getOrganizationSizeReporting")
    def get(self, orgname):
        orgperm = OrganizationMemberPermission(orgname)

        if not orgperm.can():
            raise Unauthorized()

        return {
            "response": model.namespacequota.get_namespace_repository_sizes_and_cache(orgname)
        }, 200


@resource("/v1/user/quota")
@show_if(features.QUOTA_MANAGEMENT)
class UserQuota(ApiResource):
    @require_user_admin
    @nickname("getUserQuota")
    def get(self):
        parent = get_authenticated_user()
        quota = model.namespacequota.get_namespace_quota(parent)
        quota_limit_types = model.namespacequota.get_namespace_limit_types()

        return {
            "limit_bytes": quota.limit_bytes if quota else None,
            "quota_limit_types": quota_limit_types,
        }


@resource("/v1/user/quota/limits")
@show_if(features.QUOTA_MANAGEMENT)
class UserQuotaLimits(ApiResource):
    @require_user_admin
    @nickname("getUserQuotaLimit")
    def get(self):
        parent = get_authenticated_user()
        quota_limits = list(model.namespacequota.get_namespace_limits(parent))

        return {
            "quota_limits": [{
                "percent_of_limit": limit["percent_of_limit"],
                "limit_type": {
                    "name": limit["name"],
                    "quota_limit_id": limit["id"],
                    "quota_type_id": limit["type_id"],
                },
            } for limit in quota_limits]
        }, 200


@resource("/v1/user/quota/report")
@show_if(features.QUOTA_MANAGEMENT)
class UserQuotaReport(ApiResource):
    @require_user_admin
    @nickname("getUserSizeReporting")
    def get(self):
        parent = get_authenticated_user()
        return {
            "response": model.namespacequota.get_namespace_repository_sizes_and_cache(parent)
        }, 200
