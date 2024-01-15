"""Create manifest jsonb column

Revision ID: b133e86a0665
Revises: 8d47693829a0
Create Date: 2024-01-11 10:51:07.443653

"""

# revision identifiers, used by Alembic.
revision = 'b133e86a0665'
down_revision = '8d47693829a0'

import sqlalchemy as sa
from sqlalchemy.dialects import postgresql
from sqlalchemy.engine.reflection import Inspector

def upgrade(op, tables, tester):
    bind = op.get_bind()
    if bind.engine.name == "postgresql":
        op.add_column(
            "manifest", sa.Column("manifest_json", postgresql.JSONB())
        )


def downgrade(op, tables, tester):
    bind = op.get_bind()

    if bind.engine.name == "postgresql":
        op.drop_column("manifest", "manifest_json")
