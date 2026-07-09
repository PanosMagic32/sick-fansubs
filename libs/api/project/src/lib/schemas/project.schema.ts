import { Prop, Schema, SchemaFactory } from '@nestjs/mongoose';
import mongoose, { Document } from 'mongoose';

import { User } from '@api/user';

export type ProjectDocument = Project & Document;

class BatchDownloadLink {
  @Prop({ required: true })
  name!: string;

  @Prop()
  downloadLinkTorrent?: string;

  @Prop()
  downloadLink?: string;

  @Prop()
  downloadLink4kTorrent?: string;

  @Prop()
  downloadLink4k?: string;
}

@Schema({ collection: 'projects', timestamps: true })
export class Project {
  @Prop({ required: true })
  title!: string;

  @Prop({ required: true, unique: true })
  slug!: string;

  @Prop({ required: true })
  description!: string;

  @Prop({ required: true })
  thumbnail!: string;

  @Prop({ required: true })
  dateTimeCreated!: string;

  @Prop({ type: mongoose.Schema.Types.ObjectId, ref: 'User' })
  creator!: User;

  @Prop({ type: mongoose.Schema.Types.ObjectId, ref: 'User' })
  updatedBy?: User;

  @Prop({
    type: [
      {
        name: { type: String, required: true },
        downloadLinkTorrent: { type: String },
        downloadLink: { type: String },
        downloadLink4kTorrent: { type: String },
        downloadLink4k: { type: String },
      },
    ],
    default: [],
  })
  batchDownloadLinks!: BatchDownloadLink[];
}

export const ProjectSchema = SchemaFactory.createForClass(Project);
